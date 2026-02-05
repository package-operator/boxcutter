package ownerhandling

import (
	"slices"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	"pkg.package-operator.run/boxcutter/machinery/types"
)

// Ensure NativeRevisionMetadata implements RevisionMetadata.
var _ types.RevisionMetadata = (*NativeRevisionMetadata)(nil)

// NativeRevisionMetadata uses .metadata.ownerReferences for ownership tracking.
type NativeRevisionMetadata struct {
	owner  client.Object
	scheme *runtime.Scheme
}

// NewNativeRevisionMetadata creates a RevisionMetadata using native ownerReferences.
// If allowCrossNamespace is false, only objects in owner.GetNamespace() are allowed.
// Panics if owner has an empty UID (not persisted to cluster).
func NewNativeRevisionMetadata(
	owner client.Object,
	scheme *runtime.Scheme,
) types.RevisionMetadata {
	if len(owner.GetUID()) == 0 {
		panic("owner must be persisted to cluster, empty UID")
	}

	return &NativeRevisionMetadata{
		owner:  owner,
		scheme: scheme,
	}
}

// GetReconcileOptions returns a set of options that will added to any
// revision reconciliation options.
// For native ownership, there are no default reconciliation options.
func (m *NativeRevisionMetadata) GetReconcileOptions() []types.RevisionReconcileOption {
	return nil
}

// GetTeardownOptions returns a set of options that will added to any
// revision teardown options.
func (m *NativeRevisionMetadata) GetTeardownOptions() []types.RevisionTeardownOption {
	var opts []types.RevisionTeardownOption

	// The revision owner is currently being deleted with "cascade=orphan". This
	// option was requested by the user at the time the object was deleted. It
	// was added by kube-apiserver, and is handled by the kubernetes garbage
	// collector. The garbage collector will remove owner references to this
	// object from all dependents before removing this finalizer.
	//
	// We want to honour the user's intent to orphan the dependents of this
	// object during teardown if we find ourselves racing with the garbage
	// collector. If the GC has already removed the owner reference from an
	// object, existing checks in object teardown will already ensure we ignore
	// that object. To ensure correct handling of objects which have not yet
	// been orphaned by the GC, we add the WithOrphan option to the teardown
	// options.
	if controllerutil.ContainsFinalizer(m.owner, "orphan") {
		opts = append(opts, types.WithOrphan())
	}

	return opts
}

// GetOwner returns the owner object used to create this metadata.
// This method is not part of the RevisionMetadata interface, but is currently
// used by the reference controller.
func (m *NativeRevisionMetadata) GetOwner() client.Object {
	return m.owner
}

// SetCurrent updates obj to mark this RevisionMetadata as the current (controlling) revision.
// Returns an error if the object already has a different current revision
// unless the WithAllowUpdate option is given.
func (m *NativeRevisionMetadata) SetCurrent(obj metav1.Object, opts ...types.SetCurrentOption) error {
	options := &types.SetCurrentOptions{}
	for _, opt := range opts {
		opt(options)
	}

	if options.AllowUpdate {
		ownerRefs := obj.GetOwnerReferences()
		for i := range ownerRefs {
			if ownerRefs[i].Controller != nil && *ownerRefs[i].Controller {
				ownerRefs[i].Controller = nil
			}
		}

		obj.SetOwnerReferences(ownerRefs)
	}

	return controllerutil.SetControllerReference(m.owner, obj, m.scheme)
}

// IsCurrent returns true if this RevisionMetadata is the current (controlling) revision of obj.
func (m *NativeRevisionMetadata) IsCurrent(obj metav1.Object) bool {
	ownerRefComp := nativeOwnerRefForCompare(m.owner, m.scheme)
	for _, ownerRef := range obj.GetOwnerReferences() {
		if m.referSameObject(ownerRefComp, ownerRef) &&
			ownerRef.Controller != nil &&
			*ownerRef.Controller {
			return true
		}
	}

	return false
}

// RemoveFrom removes this RevisionMetadata from obj, whether it is the current revision or otherwise.
func (m *NativeRevisionMetadata) RemoveFrom(obj metav1.Object) {
	ownerRefComp := nativeOwnerRefForCompare(m.owner, m.scheme)
	ownerRefs := obj.GetOwnerReferences()
	foundIndex := -1

	for i, ownerRef := range ownerRefs {
		if m.referSameObject(ownerRefComp, ownerRef) {
			foundIndex = i

			break
		}
	}

	if foundIndex != -1 {
		obj.SetOwnerReferences(slices.Delete(ownerRefs, foundIndex, foundIndex+1))
	}
}

// IsNamespaceAllowed returns true if objects may be created/managed in the namespace of obj.
func (m *NativeRevisionMetadata) IsNamespaceAllowed(obj metav1.Object) bool {
	ownerNs := m.owner.GetNamespace()
	// If owner is cluster-scoped, all namespaces are allowed.
	if len(ownerNs) == 0 {
		return true
	}

	// For namespaced owners, object must be in the same namespace.
	return obj.GetNamespace() == ownerNs
}

// CopyReferences copies all revision metadata from objA to objB except the current revision marker.
// This is used when taking over control from a previous owner while preserving their watch references.
func (m *NativeRevisionMetadata) CopyReferences(oldObj, newObj metav1.Object) {
	// Copy owner references from A to B.
	oldOwnerRefs := slices.Clone(oldObj.GetOwnerReferences())
	newObj.SetOwnerReferences(oldOwnerRefs)

	// Release controller on B (set all Controller fields to false).
	newOwnerRefs := newObj.GetOwnerReferences()
	for i := range newOwnerRefs {
		newOwnerRefs[i].Controller = ptr.To(false)
	}

	newObj.SetOwnerReferences(newOwnerRefs)
}

// GetCurrent returns a RevisionReference describing the current revision of obj.
// Returns nil if there is no current revision set.
func (m *NativeRevisionMetadata) GetCurrent(obj metav1.Object) types.RevisionReference {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.Controller != nil && *ref.Controller {
			// Return a copy to avoid mutation.
			refCopy := ref

			return &refCopy
		}
	}

	return nil
}

func nativeOwnerRefForCompare(obj client.Object, scheme *runtime.Scheme) metav1.OwnerReference {
	// Create a new owner ref.
	gvk, err := apiutil.GVKForObject(obj, scheme)
	if err != nil {
		panic(err)
	}

	ref := metav1.OwnerReference{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		UID:        obj.GetUID(),
		Name:       obj.GetName(),
	}

	return ref
}

func (m *NativeRevisionMetadata) referSameObject(a, b metav1.OwnerReference) bool {
	aGV, err := schema.ParseGroupVersion(a.APIVersion)
	if err != nil {
		panic(err)
	}

	bGV, err := schema.ParseGroupVersion(b.APIVersion)
	if err != nil {
		panic(err)
	}

	return aGV.Group == bGV.Group && a.Kind == b.Kind && a.Name == b.Name && a.UID == b.UID
}

// NativeEnqueueRequestForOwner returns an EventHandler that enqueues reconcile requests
// for the owner of the object that triggered the event.
func NativeEnqueueRequestForOwner(
	scheme *runtime.Scheme,
	mapper meta.RESTMapper,
	ownerType client.Object,
	isController bool,
) handler.EventHandler {
	if isController {
		return handler.EnqueueRequestForOwner(scheme, mapper, ownerType, handler.OnlyControllerOwner())
	}

	return handler.EnqueueRequestForOwner(scheme, mapper, ownerType)
}
