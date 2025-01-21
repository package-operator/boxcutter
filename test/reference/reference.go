// Package reference contains a compact reference implementation to exercise all functions of the library.
package reference

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"pkg.package-operator.run/boxcutter/machinery"
	"pkg.package-operator.run/boxcutter/machinery/ownerhandling"
	"pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/machinery/validation"
	"pkg.package-operator.run/boxcutter/managedcache"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

const (
	fieldOwner   = "boxcutter.test"
	systemPrefix = "boxcutter.test"
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
	// cm := &corev1.ConfigMap{}
	mgr, err := ctrl.NewManager(r.restConfig, ctrl.Options{
		WebhookServer: webhook.NewServer(webhook.Options{Port: 9443}),
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&corev1.ConfigMap{}: {
					Label: labels.SelectorFromSet(labels.Set{
						"package-operator.run/test-type": "Revision",
					}),
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

	// Create a remote client that does not cache resources cluster-wide.
	uncachedClient, err := client.New(
		r.restConfig, client.Options{Scheme: mgr.GetScheme(), Mapper: mgr.GetRESTMapper()})
	if err != nil {
		return fmt.Errorf("unable to set up uncached client: %w", err)
	}

	mapper := func(ctx context.Context, cm *corev1.ConfigMap, c *rest.Config, o cache.Options) (*rest.Config, cache.Options, error) {

		return c, o, nil
	}
	mc := managedcache.NewObjectBoundCache[*corev1.ConfigMap](r.scheme, mapper, r.restConfig, cache.Options{})
	if err := mgr.Add(mc); err != nil {
		return fmt.Errorf("adding managedcache: %w", err)
	}

	c := &CMRevisionReconciler{
		client:          mgr.GetClient(),
		uncachedClient:  uncachedClient,
		discoveryClient: discoveryClient,
		restMapper:      mgr.GetRESTMapper(),
		cache:           mc,
		scheme:          r.scheme,
	}
	if err := c.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setting up revision controller: %w", err)
	}

	return mgr.Start(ctx)
}

type CMRevisionReconciler struct {
	client          client.Client
	uncachedClient  client.Client
	discoveryClient *discovery.DiscoveryClient
	restMapper      meta.RESTMapper

	cache  managedcache.ObjectBoundCache[*corev1.ConfigMap]
	scheme *runtime.Scheme
}

func (c *CMRevisionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	revisionCM := &corev1.ConfigMap{}
	if err := c.client.Get(
		ctx, req.NamespacedName, revisionCM); err != nil {
		return res, client.IgnoreNotFound(err)
	}

	cache, err := c.cache.Get(ctx, revisionCM)
	if err != nil {
		return res, fmt.Errorf("get cache: %w", err)
	}

	os := ownerhandling.NewNative(c.scheme)
	comp := machinery.NewComparator(os, c.discoveryClient, c.scheme, fieldOwner)
	pval := validation.NewNamespacedPhaseValidator(c.restMapper, c.client)
	rval := validation.NewRevisionValidator()

	oe := machinery.NewObjectEngine(
		c.scheme, cache, c.uncachedClient,
		c.client, os, comp, fieldOwner, systemPrefix,
	)
	pe := machinery.NewPhaseEngine(oe, pval)

	re := machinery.NewRevisionEngine(pe, rval, c.client)

	rres, err := re.Reconcile(ctx, c.toRevision(revisionCM))
	if err != nil {
		return res, fmt.Errorf("revision reconcile: %w", err)
	}

	fmt.Println("-----------")
	fmt.Println(rres.String())

	return res, nil
}

func (c *CMRevisionReconciler) toRevision(cm *corev1.ConfigMap) types.Revision {
	return &types.RevisionStandin{
		Name:     cm.Name,
		Owner:    cm,
		Revision: 1,
		Phases: []types.Phase{
			&types.PhaseStandin{
				Name: "phase-1",
				Objects: []types.PhaseObject{
					{
						Object: &unstructured.Unstructured{
							Object: map[string]interface{}{
								"apiVersion": "v1",
								"kind":       "ConfigMap",
								"metadata": map[string]interface{}{
									"name":      "test-rev-obj-1",
									"namespace": "default",
								},
							},
						},
					},
				},
			},
			&types.PhaseStandin{
				Name: "phase-2",
				Objects: []types.PhaseObject{
					{
						Object: &unstructured.Unstructured{
							Object: map[string]interface{}{
								"apiVersion": "v1",
								"kind":       "ConfigMap",
								"metadata": map[string]interface{}{
									"name":      "test-rev-obj-2",
									"namespace": "default",
								},
							},
						},
					},
				},
			},
		},
	}
}

func (c *CMRevisionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ConfigMap{}).
		WatchesRawSource(
			c.cache.Source(
				handler.EnqueueRequestForOwner(mgr.GetScheme(), mgr.GetRESTMapper(), &corev1.ConfigMap{}),
				predicate.NewPredicateFuncs(func(object client.Object) bool {
					return true
				}),
			),
		).
		Complete(c)
}
