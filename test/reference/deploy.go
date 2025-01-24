package reference

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/utils/ptr"
	"pkg.package-operator.run/boxcutter/machinery"
	"pkg.package-operator.run/boxcutter/machinery/ownerhandling"
	bctypes "pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/machinery/validation"
	"pkg.package-operator.run/boxcutter/managedcache"
	"pkg.package-operator.run/boxcutter/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const deploymentLabel = "boxcutter.test/deployment"
const hashAnnotation = "boxcutter.test/hash"
const cmPhasesKey = "phases"
const cmRevisionNumberKey = "revision"
const cmPreviousKey = "previous"
const cmStateKey = "state"

type Reconciler struct {
	client          client.Client
	discoveryClient *discovery.DiscoveryClient
	restMapper      meta.RESTMapper

	cache  managedcache.ObjectBoundAccessManager[*corev1.ConfigMap]
	scheme *runtime.Scheme
}

func NewReconciler(
	client client.Client,
	discoveryClient *discovery.DiscoveryClient,
	restMapper meta.RESTMapper,
	cache managedcache.ObjectBoundAccessManager[*corev1.ConfigMap],
	scheme *runtime.Scheme,
) *Reconciler {
	return &Reconciler{
		client:          client,
		discoveryClient: discoveryClient,
		restMapper:      restMapper,
		cache:           cache,
		scheme:          scheme,
	}
}

func (c *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	cm := &corev1.ConfigMap{}
	if err := c.client.Get(
		ctx, req.NamespacedName, cm); err != nil {
		return res, client.IgnoreNotFound(err)
	}

	switch cm.Labels[typeLabel] {
	case "Revision":
		// every time a revision completes, check revisions for cleanup/archival.
		owner, ownerFound := getOwner(cm)
		if !ownerFound {
			// We are orphaned.
			if err := c.removeFinalizer(ctx, cm, teardownFinalizer); err != nil {
				return res, err
			}
			return res, c.client.Delete(ctx, cm, client.PropagationPolicy(metav1.DeletePropagationOrphan), client.Preconditions{
				UID:             ptr.To(cm.GetUID()),
				ResourceVersion: ptr.To(cm.GetResourceVersion()),
			})
		}
		return c.handleRevision(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				UID:       owner.UID,
				Name:      owner.Name,
				Namespace: cm.GetNamespace(),
			},
		}, cm)

	case "Deployment":
		return c.handleDeployment(ctx, cm)
	}
	return res, nil
}

func (c *Reconciler) handleDeployment(ctx context.Context, cm *corev1.ConfigMap) (res ctrl.Result, err error) {
	existingRevisionsRaw := &corev1.ConfigMapList{}
	if err := c.client.List(ctx, existingRevisionsRaw, client.MatchingLabels{
		typeLabel:       "Revision",
		deploymentLabel: cm.Name,
	}); err != nil {
		return res, fmt.Errorf("listing revisions: %w", err)
	}

	if !cm.DeletionTimestamp.IsZero() {
		if err := c.cache.Free(ctx, cm); err != nil {
			return res, fmt.Errorf("free cache: %w", err)
		}
		if err := c.removeFinalizer(ctx, cm, cacheFinalizer); err != nil {
			return res, err
		}
		return res, nil
	}

	if err := c.ensureFinalizer(ctx, cm, cacheFinalizer); err != nil {
		return res, err
	}

	var existingRevisions []bctypes.Revision
	for _, rev := range existingRevisionsRaw.Items {
		r, _, err := c.toRevision(cm.Name, &rev)
		if err != nil {
			return res, fmt.Errorf("to revision: %w", err)
		}
		existingRevisions = append(existingRevisions, r)
	}
	sort.Sort(revisionAscending(existingRevisions))

	currentHash := util.ComputeSHA256Hash(cm.Data, nil)

	// Sort into current and previous revisions.
	var (
		currentRevision bctypes.Revision
		prevRevisions   []bctypes.Revision
	)
	if len(existingRevisions) > 0 {
		maybeCurrentObjectSet := existingRevisions[len(existingRevisions)-1]
		annotations := maybeCurrentObjectSet.GetClientObject().GetAnnotations()
		if annotations != nil {
			if hash, ok := annotations[hashAnnotation]; ok &&
				hash == currentHash {
				currentRevision = maybeCurrentObjectSet
				prevRevisions = existingRevisions[0 : len(existingRevisions)-1] // previous is everything excluding current
			}
		}
	}
	if currentRevision == nil {
		// all ObjectSets are outdated.
		prevRevisions = existingRevisions
		revisionNumber := latestRevisionNumber(prevRevisions)
		revisionNumber++

		newRevision := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%d", cm.Name, revisionNumber),
				Namespace: cm.Namespace,
				Labels: map[string]string{
					typeLabel:       "Revision",
					deploymentLabel: cm.Name,
				},
				Annotations: map[string]string{
					hashAnnotation: currentHash,
				},
			},
			Data: cm.Data,
		}
		newRevision.Data[cmRevisionNumberKey] = fmt.Sprintf("%d", revisionNumber)
		newRevision.Data[cmPreviousKey] = prevJson(prevRevisions)

		if err := controllerutil.SetControllerReference(cm, newRevision, c.scheme); err != nil {
			return res, fmt.Errorf("set ownerref: %w", err)
		}
		if err := c.client.Create(ctx, newRevision); err != nil {
			return res, fmt.Errorf("creating new Revision: %w", err)
		}
	}

	return res, nil
}

func (c *Reconciler) handleRevision(
	ctx context.Context, deploy *corev1.ConfigMap, revisionCM *corev1.ConfigMap,
) (res ctrl.Result, err error) {
	accessor, err := c.cache.Get(ctx, deploy)
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

	revision, previous, err := c.toRevision(deploy.Name, revisionCM)
	if err != nil {
		return res, fmt.Errorf("converting CM to revision: %w", err)
	}

	if !revisionCM.DeletionTimestamp.IsZero() ||
		revisionCM.Data[cmStateKey] == "Archived" {
		tres, err := re.Teardown(ctx, revision)
		if err != nil {
			return res, fmt.Errorf("revision teardown: %w", err)
		}

		fmt.Println("-----------")
		fmt.Printf("%q %s\n", revisionCM.Name, tres.String())

		if !tres.IsComplete() {
			return res, nil
		}

		if err := c.removeFinalizer(ctx, revisionCM, teardownFinalizer); err != nil {
			return res, err
		}

		return res, nil
	}

	if err := c.ensureFinalizer(ctx, revisionCM, teardownFinalizer); err != nil {
		return res, err
	}

	rres, err := re.Reconcile(ctx, revision)
	if err != nil {
		return res, fmt.Errorf("revision reconcile: %w", err)
	}

	fmt.Println("-----------")
	fmt.Printf("%q %s\n", revisionCM.Name, rres.String())

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

	// Archive other revisions.
	if rres.IsComplete() {
		for _, a := range previous {
			if err := c.client.Patch(ctx, a, client.RawPatch(types.MergePatchType, []byte(`{"data":{"state":"Archived"}}`))); err != nil {
				return res, fmt.Errorf("archive previous Revision: %w", err)
			}
		}
	}
	return res, nil
}

func (c *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(
			&corev1.ConfigMap{},
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		WatchesRawSource(
			c.cache.Source(
				handler.EnqueueRequestForOwner(mgr.GetScheme(), mgr.GetRESTMapper(), &corev1.ConfigMap{}),
				predicate.ResourceVersionChangedPredicate{},
				predicate.NewPredicateFuncs(func(object client.Object) bool {
					return true
				}),
			),
		).
		Complete(c)
}

func (c *Reconciler) toRevision(deployName string, cm *corev1.ConfigMap) (r bctypes.Revision, previous []client.Object, err error) {
	var phases []string
	objects := map[string][]unstructured.Unstructured{}
	var previousUnstr []unstructured.Unstructured
	var revision int64
	for k, v := range cm.Data {
		if k == cmPhasesKey {
			if err := json.Unmarshal([]byte(v), &phases); err != nil {
				return nil, nil, fmt.Errorf("json unmarshal key %s: %w", k, err)
			}
			continue
		}

		if k == cmRevisionNumberKey {
			i, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return nil, nil, fmt.Errorf("parsing revision: %w", err)
			}
			revision = i
			continue
		}

		if k == cmPreviousKey {
			if err := json.Unmarshal([]byte(v), &previousUnstr); err != nil {
				return nil, nil, fmt.Errorf("json unmarshal key %s: %w", k, err)
			}
			continue
		}

		parts := strings.SplitN(k, "_", 2)
		if len(parts) != 2 {
			continue
		}
		phase := parts[0]

		obj := unstructured.Unstructured{}
		if err := json.Unmarshal([]byte(v), &obj); err != nil {
			return nil, nil, fmt.Errorf("json unmarshal key %s: %w", k, err)
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
		labels[deploymentLabel] = deployName
		obj.SetLabels(labels)

		objects[phase] = append(objects[phase], obj)
	}

	if revision == 0 {
		return nil, nil, fmt.Errorf("revision not set")
	}

	rev := &bctypes.RevisionStandin{
		Name:     cm.Name,
		Owner:    cm,
		Revision: revision,
	}

	for _, obj := range previousUnstr {
		previous = append(previous, &obj)
	}

	for _, phase := range phases {
		p := &bctypes.PhaseStandin{
			Name: phase,
		}
		for _, obj := range objects[phase] {
			p.Objects = append(p.Objects, bctypes.PhaseObject{
				Object: &obj,
				Opts: []bctypes.ObjectOption{
					bctypes.WithPreviousOwners(previous),
					bctypes.WithProbe(machinery.ProgressProbeType, &bctypes.ProbeFn{Fn: func(obj client.Object) (success bool, messages []string) {
						u, ok := obj.(*unstructured.Unstructured)
						if obj.GetObjectKind().GroupVersionKind().Kind != "ConfigMap" || !ok {
							return true, nil
						}
						f, ok, _ := unstructured.NestedString(u.Object, "data", "continue")
						if !ok {
							return false, []string{".data.continue not set"}
						}
						if f != "yes" {
							return false, []string{`.data.continue not set to "yes"`}
						}
						return true, nil
					}}),
				},
			})
		}
		rev.Phases = append(rev.Phases, p)
	}

	return rev, previous, nil
}

func (c *Reconciler) ensureFinalizer(
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

func (c *Reconciler) removeFinalizer(
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
