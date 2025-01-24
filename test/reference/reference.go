// Package reference contains a compact reference implementation to exercise all functions of the library.
package reference

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"pkg.package-operator.run/boxcutter/managedcache"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

const (
	fieldOwner        = "boxcutter.test"
	systemPrefix      = "boxcutter.test"
	cacheFinalizer    = "boxcutter.test/cache"
	teardownFinalizer = "boxcutter.test/teardown"
	typeLabel         = "boxcutter.test/type"
)

type Reference struct {
	scheme     *runtime.Scheme
	restConfig *rest.Config
}

func NewReference(
	scheme *runtime.Scheme,
	restConfig *rest.Config,
) *Reference {
	return &Reference{
		scheme:     scheme,
		restConfig: restConfig,
	}
}

// Starts the reference controller and blocks until the context is cancelled.
func (r *Reference) Start(ctx context.Context) error {
	req, err := labels.NewRequirement(typeLabel, selection.In, []string{"Revision", "Deployment"})
	if err != nil {
		return err
	}
	cmselector := labels.NewSelector().Add(*req)

	mgr, err := ctrl.NewManager(r.restConfig, ctrl.Options{
		WebhookServer: webhook.NewServer(webhook.Options{Port: 9443}),
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&corev1.ConfigMap{}: {
					Label: cmselector,
				},
			},
		},
		Metrics:                server.Options{BindAddress: "0"},
		Scheme:                 r.scheme,
		HealthProbeBindAddress: "0",
		LeaderElection:         false,
	})
	if err != nil {
		return fmt.Errorf("creating manager: %w", err)
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(r.restConfig)
	if err != nil {
		return fmt.Errorf("creating discovery client: %w", err)
	}

	mapper := func(ctx context.Context, cm *corev1.ConfigMap, c *rest.Config, o cache.Options) (*rest.Config, cache.Options, error) {
		req1, err := labels.NewRequirement(deploymentLabel, selection.Equals, []string{cm.Name})
		if err != nil {
			return nil, o, err
		}
		req2, err := labels.NewRequirement(typeLabel, selection.DoesNotExist, []string{})
		if err != nil {
			return nil, o, err
		}
		dynSelector := labels.NewSelector().Add(*req1, *req2)

		o.DefaultLabelSelector = dynSelector
		o.DefaultNamespaces = map[string]cache.Config{
			cm.Namespace: {},
		}
		c.Impersonate = rest.ImpersonationConfig{
			UID:      string(cm.GetUID()),
			UserName: fmt.Sprintf("boxcutter:reference:%s:%s", cm.GetNamespace(), cm.GetName()),
			Groups: []string{
				"boxcutter:references:" + cm.GetNamespace(),
				"boxcutter:references",
			},
		}
		return c, o, nil
	}
	mc := managedcache.NewObjectBoundAccessManager[*corev1.ConfigMap](mapper, r.restConfig, cache.Options{
		Scheme: r.scheme, Mapper: mgr.GetRESTMapper(),
	})
	if err := mgr.Add(mc); err != nil {
		return fmt.Errorf("adding managedcache: %w", err)
	}

	c := NewReconciler(mgr.GetClient(), discoveryClient, mgr.GetRESTMapper(), mc, r.scheme)
	if err := c.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setting up deployment controller: %w", err)
	}

	return mgr.Start(ctx)
}
