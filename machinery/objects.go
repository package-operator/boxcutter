package machinery

import (
	"context"
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	machinerytypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/csaupgrade"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjectEngine reconciles individual objects.
type ObjectEngine struct {
	cache objectEngineCache
	// Used to resolve conflicts with objects
	// excluded by the cache selector.
	uncachedReader  client.Reader
	writer          client.Writer
	ownerStrategy   objectEngineOwnerStrategy
	divergeDetector objectEngineDivergeDetector

	fieldOwner   string
	systemPrefix string
}

// NewObjectEngine returns a new Engine instance.
func NewObjectEngine(
	cache objectEngineCache,
	uncachedReader client.Reader,
	writer client.Writer,
	ownerStrategy objectEngineOwnerStrategy,
	divergeDetector objectEngineDivergeDetector,

	fieldOwner string,
	systemPrefix string,
) *ObjectEngine {
	return &ObjectEngine{
		cache:           cache,
		uncachedReader:  uncachedReader,
		writer:          writer,
		ownerStrategy:   ownerStrategy,
		divergeDetector: divergeDetector,

		fieldOwner:   fieldOwner,
		systemPrefix: systemPrefix,
	}
}

type objectEngineCache interface {
	client.Reader

	// Called to inform cache about owner object relationships.
	// Allows cache to dynamically setup and teardown caches.
	// This method should block until this cache has been established and synced.
	Watch(
		ctx context.Context, owner client.Object, obj runtime.Object,
	) error
}

type objectEngineOwnerStrategy interface {
	SetControllerReference(owner, obj metav1.Object) error
	GetController(obj metav1.Object) (metav1.OwnerReference, bool)
	IsController(owner, obj metav1.Object) bool
	CopyOwnerReferences(objA, objB metav1.Object)
	ReleaseController(obj metav1.Object)
}

type objectEngineDivergeDetector interface {
	HasDiverged(
		owner client.Object,
		desiredObject, actualObject *unstructured.Unstructured,
	) (res DivergeResult, err error)
}

// Teardown ensures the given object is safely removed from the cluster.
func (e *ObjectEngine) Teardown(
	ctx context.Context,
	owner client.Object, // Owner of the object.
	revision int64, // Revision number, must start at 1.
	desiredObject *unstructured.Unstructured,
) (objectDeleted bool, err error) {
	// Sanity checks.
	if revision == 0 {
		panic("owner revision must be set and start at 1")
	}
	if len(owner.GetUID()) == 0 {
		panic("owner must be persisted to cluster, empty UID")
	}

	// Ensure to prime cache.
	err = e.cache.Watch(
		ctx, owner, desiredObject)
	if meta.IsNoMatchError(err) {
		// API no longer registered.
		// Consider the object deleted.
		return true, nil
	} else if err != nil {
		return false, fmt.Errorf("watching resource: %w", err)
	}

	// Lookup actual object state on cluster.
	actualObject := desiredObject.DeepCopy()
	err = e.cache.Get(
		ctx, client.ObjectKeyFromObject(desiredObject), actualObject,
	)
	if errors.IsNotFound(err) {
		// Object is gone, yay!
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("getting object before deletion: %w", err)
	}

	// Check revision matches.
	actualRevision, err := e.getObjectRevision(actualObject)
	if err != nil {
		return false, fmt.Errorf("getting object revision: %w", err)
	}
	if actualRevision != revision {
		return false, TeardownRevisionError{
			msg: fmt.Sprintf(
				"Rejecting object teardown: Expected revision %d, actual revision %d",
				revision, actualRevision,
			),
		}
	}

	ctrlSit, _ := e.detectOwner(owner, actualObject, nil)
	if ctrlSit != ctrlSituationIsController {
		return false, TeardownRevisionError{
			msg: "Rejecting object teardown: Owner not controller",
		}
	}

	// Actually delete the object.
	err = e.writer.Delete(ctx, desiredObject, client.Preconditions{
		UID:             ptr.To(actualObject.GetUID()),
		ResourceVersion: ptr.To(actualObject.GetResourceVersion()),
	})
	if errors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("deleting object: %w", err)
	}
	// need to wait for Not Found Error to ensure finalizers have been progressed.
	return false, nil
}

// Reconcile runs actions to bring actual state closer to desired.
func (e *ObjectEngine) Reconcile(
	ctx context.Context,
	owner client.Object, // Owner of the object.
	revision int64, // Revision number, must start at 1.
	desiredObject *unstructured.Unstructured,
	opts ...ObjectOption,
) (ObjectResult, error) {
	var options ObjectOptions
	for _, opt := range opts {
		opt.ApplyToObjectOptions(&options)
	}
	options.Default()

	// Sanity checks.
	if revision == 0 {
		panic("owner revision must be set and start at 1")
	}
	if len(owner.GetUID()) == 0 {
		panic("owner must be persistet to cluster, empty UID")
	}

	// Copy because some client actions will modify the object.
	desiredObject = desiredObject.DeepCopy()
	e.setObjectRevision(desiredObject, revision)
	if err := e.ownerStrategy.SetControllerReference(
		owner, desiredObject,
	); err != nil {
		return nil, fmt.Errorf("set controller reference: %w", err)
	}

	// Ensure to prime cache.
	if err := e.cache.Watch(
		ctx, owner, desiredObject); err != nil {
		return nil, fmt.Errorf("watching resource: %w", err)
	}

	// Lookup actual object state on cluster.
	actualObject := desiredObject.DeepCopy()
	err := e.cache.Get(
		ctx, client.ObjectKeyFromObject(desiredObject), actualObject,
	)
	switch {
	case errors.IsNotFound(err):
		// Object might still already exist on the cluster,
		// either because of slow caches or because
		// label selectors exclude it from the cache.
		//
		// To be on the safe-side do a normal POST call.
		// Using SSA might patch an already existing object,
		// violating collision protection settings.
		err := e.create(
			ctx, desiredObject, options, client.FieldOwner(e.fieldOwner))
		if errors.IsAlreadyExists(err) {
			// Might be a slow cache or an object created by a different actor
			// but excluded by the cache selector.
			if err := e.uncachedReader.Get(
				ctx, client.ObjectKeyFromObject(desiredObject), actualObject,
			); err != nil {
				return nil, fmt.Errorf("getting object via cache bypass after create conflict: %w", err)
			}
			return e.objectUpdateHandling(
				ctx, owner, revision,
				desiredObject, actualObject, options,
			)
		}
		if err != nil {
			return nil, fmt.Errorf("creating resource: %w", err)
		}
		if err := e.migrateFieldManagersToSSA(ctx, desiredObject); err != nil {
			return nil, fmt.Errorf("migrating to SSA after create: %w", err)
		}
		return newObjectResultCreated(
			desiredObject, options.Probe), nil

	case err != nil:
		return nil, fmt.Errorf("getting object: %w", err)
	}

	return e.objectUpdateHandling(
		ctx, owner, revision,
		desiredObject, actualObject, options,
	)
}

func (e *ObjectEngine) objectUpdateHandling(
	ctx context.Context,
	owner client.Object,
	revision int64,
	desiredObject *unstructured.Unstructured,
	actualObject *unstructured.Unstructured,
	options ObjectOptions,
) (ObjectResult, error) {
	// An object already exists on the cluster.
	// Before doing anything else, we have to figure out
	// who owns and controls the object.
	ctrlSit, actualOwner := e.detectOwner(owner, actualObject, options.PreviousOwners)
	diverged, err := e.divergeDetector.HasDiverged(owner, desiredObject, actualObject)
	if err != nil {
		return nil, fmt.Errorf("diverge check: %w", err)
	}

	// Ensure revision linearity.
	actualObjectRevision, err := e.getObjectRevision(actualObject)
	if err != nil {
		return nil, fmt.Errorf("getting revision of object: %w", err)
	}
	if actualObjectRevision > revision {
		// Leave object alone.
		// It's already owned by a later revision.
		return newObjectResultProgressed(
			actualObject, diverged, options.Probe,
		), nil
	}

	switch ctrlSit {
	case ctrlSituationIsController:
		modified := diverged.Comparison != nil &&
			(!diverged.Comparison.Added.Empty() ||
				!diverged.Comparison.Modified.Empty() ||
				!diverged.Comparison.Removed.Empty())
		if !diverged.IsConflict() && !modified {
			// No conflict with another field manager
			// and no modification needed.
			return newObjectResultIdle(
				actualObject, diverged, options.Probe,
			), nil
		}
		if !diverged.IsConflict() && modified {
			// No conflict with another controller, but modifications needed.
			err := e.patch(
				ctx, desiredObject, client.Apply,
				options,
			)
			if err != nil {
				// Might be a Conflict if object already exists.
				return nil, fmt.Errorf("patching (modified): %w", err)
			}
			return newObjectResultUpdated(
				desiredObject, diverged, options.Probe,
			), nil
		}

		// This is not supposed to happen.
		// Some other entity changed fields under our control,
		// while not contesting to be object controller!
		//
		// Let's try to force those fields back to their intended values.
		// If this change is being done by another controller tightly operating
		// on this resource, this may lead to a ownership fight.
		//
		// Note "Collision Protection":
		// We don't care about collision protection settings here,
		// because we are already controlling the object.
		//
		// Note "Concurrent Reconciles":
		// It's safe because this patch operation will fail if another reconciler
		// claimed controlling ownership in the background.
		// The failure is caused by this patch operation
		// adding this revision as controller and another controller existing.
		// Having two ownerRefs set to controller is rejected by the kube-apiserver.
		// Even though we force FIELD-level ownership in the call below.
		err := e.patch(
			ctx, desiredObject, client.Apply,
			options,
			client.ForceOwnership,
		)
		if err != nil {
			return nil, fmt.Errorf("patching (conflict): %w", err)
		}
		return newObjectResultRecovered(
			desiredObject, diverged, options.Probe,
		), nil

		// Taking control checklist:
		// - current controlling owner MUST be in PreviousOwners list
		//   - OR object has _no_ controlling owner and CollisionProtection set to IfNoController or None
		//   - OR object has another controlling owner and Collision Protection is set to None
		//
		// If any of the above points is not true, refuse.

	case ctrlSituationUnknownController:
		if options.CollisionProtection != CollisionProtectionNone {
			return newObjectResultConflict(
				actualObject, diverged,
				actualOwner, options.Probe,
			), nil
		}

	case ctrlSituationNoController:
		if options.CollisionProtection == CollisionProtectionPrevent {
			return newObjectResultConflict(
				actualObject, diverged,
				actualOwner, options.Probe,
			), nil
		}

	case ctrlSituationPreviousIsController:
		// no extra operation
	}
	// A previous revision is current controller.
	// This means we want to take control, but
	// retain older revisions ownerReferences,
	// so they can still react to events.

	// TODO:
	// ObjectResult ModifiedFields does not contain ownerReference changes
	// introduced here, this may lead to Updated Actions without modifications.
	e.setObjectRevision(desiredObject, revision)
	e.ownerStrategy.CopyOwnerReferences(actualObject, desiredObject)
	e.ownerStrategy.ReleaseController(desiredObject)
	if err := e.ownerStrategy.SetControllerReference(
		owner, desiredObject,
	); err != nil {
		return nil, fmt.Errorf("set controller reference: %w", err)
	}

	// Write changes.
	err = e.patch(
		ctx, desiredObject, client.Apply,
		options,
		client.ForceOwnership,
	)
	if err != nil {
		// Might be a Conflict if object already exists.
		return nil, fmt.Errorf("patching (owner change): %w", err)
	}
	return newObjectResultUpdated(
		desiredObject, diverged, options.Probe,
	), nil
}

func (e *ObjectEngine) create(
	ctx context.Context, obj client.Object,
	options ObjectOptions, opts ...client.CreateOption,
) error {
	if options.Paused {
		return nil
	}
	return e.writer.Create(ctx, obj, opts...)
}

func (e *ObjectEngine) patch(
	ctx context.Context,
	obj *unstructured.Unstructured,
	patch client.Patch,
	options ObjectOptions,
	opts ...client.PatchOption,
) error {
	if options.Paused {
		return nil
	}
	if err := e.migrateFieldManagersToSSA(ctx, obj); err != nil {
		return err
	}

	o := []client.PatchOption{
		client.FieldOwner(e.fieldOwner),
	}
	o = append(o, opts...)
	return e.writer.Patch(ctx, obj, patch, o...)
}

type ctrlSituation string

const (
	// Owner is already controller.
	ctrlSituationIsController ctrlSituation = "IsController"
	// Previous revision/previous owner is controller.
	ctrlSituationPreviousIsController ctrlSituation = "PreviousIsController"
	// Someone else is controller of this object.
	// This includes the "next" revision, as it's not in "previousOwners".
	ctrlSituationUnknownController ctrlSituation = "UnknownController"
	// No controller found.
	ctrlSituationNoController ctrlSituation = "NoController"
)

func (e *ObjectEngine) detectOwner(
	owner client.Object,
	actualObject *unstructured.Unstructured,
	previousOwners []client.Object,
) (ctrlSituation, *metav1.OwnerReference) {
	// e.ownerStrategy may either work on .metadata.ownerReferences or
	// on an annotation to allow cross-namespace and cross-cluster refs.
	ownerRef, ok := e.ownerStrategy.GetController(actualObject)
	if !ok {
		return ctrlSituationNoController, nil
	}

	// Are we already controller?
	if e.ownerStrategy.IsController(owner, actualObject) {
		return ctrlSituationIsController, &ownerRef
	}

	// Check if previous owner is controller.
	for _, previousOwner := range previousOwners {
		if e.ownerStrategy.IsController(previousOwner, actualObject) {
			return ctrlSituationPreviousIsController, &ownerRef
		}
	}

	// Anyone else controller?
	// This statement can only resolve to true if annotations
	// are used for owner reference tracking.
	return ctrlSituationUnknownController, &ownerRef
}

// Stores the revision number in a well-known annotation on the given object.
func (e *ObjectEngine) setObjectRevision(obj client.Object, revision int64) {
	a := obj.GetAnnotations()
	if a == nil {
		a = map[string]string{}
	}
	a[e.revisionAnnotation()] = strconv.FormatInt(revision, 10)
	obj.SetAnnotations(a)
}

// Retrieves the revision number from a well-known annotation on the given object.
func (e *ObjectEngine) getObjectRevision(obj client.Object) (int64, error) {
	a := obj.GetAnnotations()
	if a == nil {
		return 0, nil
	}
	if len(a[e.revisionAnnotation()]) == 0 {
		return 0, nil
	}
	return strconv.ParseInt(a[e.revisionAnnotation()], 10, 64)
}

// Migrate field ownerships to be compatible with server-side apply.
// SSA really is complicated: https://github.com/kubernetes/kubernetes/issues/99003
func (e *ObjectEngine) migrateFieldManagersToSSA(
	ctx context.Context, object *unstructured.Unstructured,
) error {
	patch, err := csaupgrade.UpgradeManagedFieldsPatch(
		object, sets.New(e.fieldOwner), e.fieldOwner)
	switch {
	case err != nil:
		return err
	case len(patch) == 0:
		// csaupgrade.UpgradeManagedFieldsPatch returns nil, nil when no work is to be done.
		// Empty patch cannot be applied so exit early.
		return nil
	}

	if err := e.writer.Patch(ctx, object, client.RawPatch(
		machinerytypes.JSONPatchType, patch)); err != nil {
		return fmt.Errorf("update field managers: %w", err)
	}
	return nil
}

func (e *ObjectEngine) revisionAnnotation() string {
	return e.systemPrefix + "/revision"
}
