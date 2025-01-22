// Package reference contains a compact reference implementation to exercise all functions of the library.
package reference

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"pkg.package-operator.run/boxcutter/machinery"
	"pkg.package-operator.run/boxcutter/machinery/ownerhandling"
	bctypes "pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/machinery/validation"
	"pkg.package-operator.run/boxcutter/managedcache"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

const (
	fieldOwner     = "boxcutter.test"
	systemPrefix   = "boxcutter.test"
	cacheFinalizer = "boxcutter.test/cache"
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
	mgr, err := ctrl.NewManager(r.restConfig, ctrl.Options{
		WebhookServer: webhook.NewServer(webhook.Options{Port: 9443}),
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&corev1.ConfigMap{}: {
					Label: labels.SelectorFromSet(labels.Set{
						// Label selector so our main controller does only react to specific ConfigMaps.
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

	mapper := func(ctx context.Context, cm *corev1.ConfigMap, c *rest.Config, o cache.Options) (*rest.Config, cache.Options, error) {
		o.DefaultLabelSelector = labels.SelectorFromSet(labels.Set{
			"boxcutter.test/Owner":     "ConfigMap",
			"boxcutter.test/OwnerName": cm.Name,
		})
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

	c := &CMRevisionReconciler{
		client:          mgr.GetClient(),
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
	discoveryClient *discovery.DiscoveryClient
	restMapper      meta.RESTMapper

	cache  managedcache.ObjectBoundAccessManager[*corev1.ConfigMap]
	scheme *runtime.Scheme
}

func (c *CMRevisionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	revisionCM := &corev1.ConfigMap{}
	if err := c.client.Get(
		ctx, req.NamespacedName, revisionCM); err != nil {
		return res, client.IgnoreNotFound(err)
	}

	accessor, err := c.cache.Get(ctx, revisionCM)
	if err != nil {
		return res, fmt.Errorf("get cache: %w", err)
	}

	os := ownerhandling.NewNative(c.scheme)
	comp := machinery.NewComparator(os, c.discoveryClient, c.scheme, fieldOwner)
	pval := validation.NewNamespacedPhaseValidator(c.restMapper, accessor)
	rval := validation.NewRevisionValidator()

	oe := machinery.NewObjectEngine(
		c.scheme, accessor, accessor, os, comp, fieldOwner, systemPrefix,
	)
	pe := machinery.NewPhaseEngine(oe, pval)

	re := machinery.NewRevisionEngine(pe, rval, accessor)

	rev, err := c.toRevision(revisionCM)
	if err != nil {
		return res, fmt.Errorf("converting CM to revision: %w", err)
	}

	if !revisionCM.DeletionTimestamp.IsZero() {
		tres, err := re.Teardown(ctx, rev)
		if err != nil {
			return res, fmt.Errorf("revision teardown: %w", err)
		}

		fmt.Println("-----------")
		fmt.Println(tres.String())

		if !tres.IsComplete() {
			return res, nil
		}

		if err := c.cache.Free(ctx, revisionCM); err != nil {
			return res, fmt.Errorf("freeing cache: %w", err)
		}

		if err := c.removeFinalizer(ctx, revisionCM, cacheFinalizer); err != nil {
			return res, err
		}

		return res, nil
	}

	if err := c.ensureFinalizer(ctx, revisionCM, cacheFinalizer); err != nil {
		return res, err
	}

	rres, err := re.Reconcile(ctx, rev)
	if err != nil {
		return res, fmt.Errorf("revision reconcile: %w", err)
	}

	fmt.Println("-----------")
	fmt.Println(rres.String())

	// Retry failing preflight checks with a flat 10s retry.
	if _, ok := rres.GetPreflightViolation(); ok {
		res.RequeueAfter = 10 * time.Second
		return res, nil
	}
	for _, pres := range rres.GetPhases() {
		if _, ok := pres.GetPreflightViolation(); ok {
			res.RequeueAfter = 10 * time.Second
			return res, nil
		}
	}

	return res, nil
}

func (c *CMRevisionReconciler) ensureFinalizer(
	ctx context.Context, obj client.Object, finalizer string,
) error {
	if controllerutil.ContainsFinalizer(obj, finalizer) {
		return nil
	}

	controllerutil.AddFinalizer(obj, finalizer)
	patch := map[string]any{
		"metadata": map[string]any{
			"resourceVersion": obj.GetResourceVersion(),
			"finalizers":      obj.GetFinalizers(),
		},
	}
	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshalling patch to remove finalizer: %w", err)
	}

	if err := c.client.Patch(ctx, obj, client.RawPatch(types.MergePatchType, patchJSON)); err != nil {
		return fmt.Errorf("adding finalizer: %w", err)
	}
	return nil
}

func (c *CMRevisionReconciler) removeFinalizer(
	ctx context.Context, obj client.Object, finalizer string,
) error {
	if !controllerutil.ContainsFinalizer(obj, finalizer) {
		return nil
	}

	controllerutil.RemoveFinalizer(obj, finalizer)

	patch := map[string]any{
		"metadata": map[string]any{
			"resourceVersion": obj.GetResourceVersion(),
			"finalizers":      obj.GetFinalizers(),
		},
	}
	patchJSON, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshalling patch to remove finalizer: %w", err)
	}
	if err := c.client.Patch(ctx, obj, client.RawPatch(types.MergePatchType, patchJSON)); err != nil {
		return fmt.Errorf("removing finalizer: %w", err)
	}
	return nil
}

const cmPhasesKey = "phases"

func (c *CMRevisionReconciler) toRevision(cm *corev1.ConfigMap) (bctypes.Revision, error) {
	var phases []string
	objects := map[string][]unstructured.Unstructured{}
	for k, v := range cm.Data {
		if k == cmPhasesKey {
			if err := json.Unmarshal([]byte(v), &phases); err != nil {
				return nil, fmt.Errorf("json unmarshal key %s: %w", k, err)
			}
			continue
		}

		parts := strings.SplitN(k, "_", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("malformed object key")
		}
		phase := parts[0]

		obj := unstructured.Unstructured{}
		if err := json.Unmarshal([]byte(v), &obj); err != nil {
			return nil, fmt.Errorf("json unmarshal key %s: %w", k, err)
		}

		// Default namespace to the owners namespace
		if len(obj.GetNamespace()) == 0 {
			obj.SetNamespace(
				cm.GetNamespace())
		}

		labels := obj.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels["boxcutter.test/Owner"] = "ConfigMap"
		labels["boxcutter.test/OwnerName"] = cm.Name
		obj.SetLabels(labels)

		objects[phase] = append(objects[phase], obj)
	}

	rev := &bctypes.RevisionStandin{
		Name:     cm.Name,
		Owner:    cm,
		Revision: 1,
	}

	for _, phase := range phases {
		p := &bctypes.PhaseStandin{
			Name: phase,
		}
		for _, obj := range objects[phase] {
			p.Objects = append(p.Objects, bctypes.PhaseObject{
				Object: &obj,
			})
		}
		rev.Phases = append(rev.Phases, p)
	}

	return rev, nil
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
