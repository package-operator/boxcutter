package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"pkg.package-operator.run/boxcutter"
	"pkg.package-operator.run/boxcutter/managedcache"
	"pkg.package-operator.run/boxcutter/probing"
	"pkg.package-operator.run/boxcutter/util"
)

const (
	deploymentLabel     = "boxcutter.test/deployment"
	hashAnnotation      = "boxcutter.test/hash"
	cmPhasesKey         = "phases"
	cmRevisionNumberKey = "revision"
	cmPreviousKey       = "previous"
	cmStateKey          = "state"

	revisionHistoryLimit = 5
)

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
			err := client.IgnoreNotFound(
				c.client.Delete(ctx, cm, client.PropagationPolicy(metav1.DeletePropagationOrphan), client.Preconditions{
					UID:             ptr.To(cm.GetUID()),
					ResourceVersion: ptr.To(cm.GetResourceVersion()),
				}),
			)
			if err != nil {
				return res, err
			}

			if err := c.removeFinalizer(ctx, cm, teardownFinalizer); err != nil {
				return res, err
			}

			return res, err
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

	existingRevisions := make([]boxcutter.Revision, 0, len(existingRevisionsRaw.Items))

	for _, rev := range existingRevisionsRaw.Items {
		r, _, _, err := c.toRevision(cm.Name, &rev)
		if err != nil {
			return res, fmt.Errorf("to revision: %w", err)
		}

		existingRevisions = append(existingRevisions, *r)
	}

	sort.Sort(revisionAscending(existingRevisions))

	currentHash := util.ComputeSHA256Hash(cm.Data, nil)

	// Sort into current and previous revisions.
	var (
		currentRevision *boxcutter.Revision
		prevRevisions   []boxcutter.Revision
	)

	if len(existingRevisions) > 0 {
		maybeCurrentObjectSet := existingRevisions[len(existingRevisions)-1]

		annotations := maybeCurrentObjectSet.GetOwner().GetAnnotations()
		if annotations != nil {
			if hash, ok := annotations[hashAnnotation]; ok &&
				hash == currentHash {
				currentRevision = &maybeCurrentObjectSet
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
		newRevision.Data[cmRevisionNumberKey] = strconv.FormatInt(revisionNumber, 10)
		newRevision.Data[cmPreviousKey] = prevJSON(prevRevisions)

		if err := controllerutil.SetControllerReference(cm, newRevision, c.scheme); err != nil {
			return res, fmt.Errorf("set ownerref: %w", err)
		}

		if err := c.client.Create(ctx, newRevision); err != nil {
			return res, fmt.Errorf("creating new Revision: %w", err)
		}
	}

	// Delete archived previous revisions over revisionHistory limit
	numToDelete := len(prevRevisions) - revisionHistoryLimit
	slices.Reverse(prevRevisions)

	for _, prevRev := range prevRevisions {
		if numToDelete <= 0 {
			break
		}

		if err := client.IgnoreNotFound(c.client.Delete(ctx, prevRev.GetOwner())); err != nil {
			return res, fmt.Errorf("failed to delete revision (history limit): %w", err)
		}

		numToDelete--
	}

	return res, nil
}

func (c *Reconciler) handleRevision(
	ctx context.Context, deploy *corev1.ConfigMap, revisionCM *corev1.ConfigMap,
) (res ctrl.Result, err error) {
	revision, opts, previous, err := c.toRevision(deploy.Name, revisionCM)
	if err != nil {
		return res, fmt.Errorf("converting CM to revision: %w", err)
	}

	var objects []client.Object

	for _, phase := range revision.GetPhases() {
		for _, pobj := range phase.GetObjects() {
			objects = append(objects, &pobj)
		}
	}

	accessor, err := c.cache.GetWithUser(ctx, deploy, revisionCM, objects)
	if err != nil {
		return res, fmt.Errorf("get cache: %w", err)
	}

	re, err := boxcutter.NewRevisionEngine(boxcutter.RevisionEngineOptions{
		Scheme:          c.scheme,
		FieldOwner:      fieldOwner,
		SystemPrefix:    systemPrefix,
		DiscoveryClient: c.discoveryClient,
		RestMapper:      c.restMapper,
		Writer:          accessor,
		Reader:          accessor,
	})
	if err != nil {
		return res, fmt.Errorf("new revision engine: %w", err)
	}

	if !revisionCM.DeletionTimestamp.IsZero() ||
		revisionCM.Data[cmStateKey] == "Archived" {
		tres, err := re.Teardown(ctx, *revision)
		if err != nil {
			return res, fmt.Errorf("revision teardown: %w", err)
		}

		_, _ = fmt.Fprintln(os.Stdout, "-----------")
		_, _ = fmt.Fprintf(os.Stdout, "%q %s\n", revisionCM.Name, tres.String())

		if !tres.IsComplete() {
			return res, nil
		}

		if err := c.cache.FreeWithUser(ctx, deploy, revisionCM); err != nil {
			return res, fmt.Errorf("get cache: %w", err)
		}

		if err := c.removeFinalizer(ctx, revisionCM, teardownFinalizer); err != nil {
			return res, err
		}

		return res, nil
	}

	if err := c.ensureFinalizer(ctx, revisionCM, teardownFinalizer); err != nil {
		return res, err
	}

	rres, err := re.Reconcile(ctx, *revision, opts...)
	if err != nil {
		return res, fmt.Errorf("revision reconcile: %w", err)
	}

	_, _ = fmt.Fprintln(os.Stdout, "-----------")
	_, _ = fmt.Fprintf(os.Stdout, "%q %s\n", revisionCM.Name, rres.String())

	// Retry failing preflight checks with a flat 10s retry.
	if verr := rres.GetValidationError(); verr != nil {
		res.RequeueAfter = 10 * time.Second
		//nolint:nilerr
		return res, nil
	}

	for _, pres := range rres.GetPhases() {
		if verr := pres.GetValidationError(); verr != nil {
			res.RequeueAfter = 10 * time.Second
			//nolint:nilerr
			return res, nil
		}
	}

	// Archive other revisions.
	if rres.IsComplete() {
		for _, a := range previous {
			if err := c.client.Patch(ctx, a, client.RawPatch(
				types.MergePatchType, []byte(`{"data":{"state":"Archived"}}`))); err != nil {
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
			),
		).
		Complete(c)
}

type RevisionNumberNotSetError struct {
	msg string
}

func (e RevisionNumberNotSetError) Error() string {
	return e.msg
}

func (c *Reconciler) toRevision(deployName string, cm *corev1.ConfigMap) (
	r *boxcutter.Revision, opts []boxcutter.RevisionReconcileOption, previous []client.Object, err error,
) {
	var (
		phases        []string
		previousUnstr []unstructured.Unstructured
		revision      int64
	)

	objects := map[string][]unstructured.Unstructured{}

	for k, v := range cm.Data {
		if k == cmPhasesKey {
			if err := json.Unmarshal([]byte(v), &phases); err != nil {
				return nil, nil, nil, fmt.Errorf("json unmarshal key %s: %w", k, err)
			}

			continue
		}

		if k == cmRevisionNumberKey {
			i, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("parsing revision: %w", err)
			}

			revision = i

			continue
		}

		if k == cmPreviousKey {
			if err := json.Unmarshal([]byte(v), &previousUnstr); err != nil {
				return nil, nil, nil, fmt.Errorf("json unmarshal key %s: %w", k, err)
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
			return nil, nil, nil, fmt.Errorf("json unmarshal key %s: %w", k, err)
		}

		// Force namespace to the owner's namespace.
		obj.SetNamespace(
			cm.GetNamespace())

		labels := obj.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}

		labels[deploymentLabel] = deployName
		obj.SetLabels(labels)

		objects[phase] = append(objects[phase], obj)
	}

	if revision == 0 {
		return nil, nil, nil, RevisionNumberNotSetError{msg: "revision not set"}
	}

	rev := &boxcutter.Revision{
		Name:     cm.Name,
		Owner:    cm,
		Revision: revision,
	}

	for _, obj := range previousUnstr {
		previous = append(previous, &obj)
	}

	for _, phase := range phases {
		p := boxcutter.Phase{
			Name:    phase,
			Objects: objects[phase],
		}

		rev.Phases = append(rev.Phases, p)
	}

	opts = []boxcutter.RevisionReconcileOption{
		boxcutter.WithPreviousOwners(previous),
		boxcutter.WithProbe(
			boxcutter.ProgressProbeType,
			boxcutter.ProbeFunc(func(obj client.Object) probing.Result {
				u, ok := obj.(*unstructured.Unstructured)
				if obj.GetObjectKind().GroupVersionKind().Kind != "ConfigMap" || !ok {
					return probing.Result{
						Status: probing.StatusTrue,
					}
				}
				f, ok, _ := unstructured.NestedString(u.Object, "data", "continue")
				if !ok {
					return probing.Result{
						Status:   probing.StatusFalse,
						Messages: []string{".data.continue not set"},
					}
				}
				if f != "yes" {
					return probing.Result{
						Status:   probing.StatusFalse,
						Messages: []string{`.data.continue not set to "yes"`},
					}
				}

				return probing.Result{
					Status: probing.StatusTrue,
				}
			})),
	}

	if cm.Data[cmStateKey] == "Paused" {
		opts = append(opts, boxcutter.WithPaused{})
	}

	return rev, opts, previous, nil
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
