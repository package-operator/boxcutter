package ownerhandling

import (
	"fmt"
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
	allowCrossNamespace bool,
) *NativeRevisionMetadata {
	if len(owner.GetUID()) == 0 {
		panic("owner must be persisted to cluster, empty UID")
	}

	return &NativeRevisionMetadata{
		owner:  owner,
		scheme: scheme,
	}
}

// GetOwner returns the owner object used to create this metadata.
func (m *NativeRevisionMetadata) GetOwner() client.Object {
	return m.owner
}

// SetCurrent updates obj to mark this RevisionMetadata as the current (controlling) revision.
// Returns an error if the object already has a different current revision.
func (m *NativeRevisionMetadata) SetCurrent(obj metav1.Object) error {
	return controllerutil.SetControllerReference(m.owner, obj, m.scheme)
}

// IsCurrent returns true if this RevisionMetadata is the current (controlling) revision of obj.
func (m *NativeRevisionMetadata) IsCurrent(obj metav1.Object) bool {
	ownerRefComp := m.ownerRefForCompare()
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
	ownerRefComp := m.ownerRefForCompare()
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
func (m *NativeRevisionMetadata) CopyReferences(objA, objB metav1.Object) {
	// Copy owner references from A to B.
	objB.SetOwnerReferences(objA.GetOwnerReferences())
	// Release controller on B (set all Controller fields to false).
	ownerRefs := objB.GetOwnerReferences()
	for i := range ownerRefs {
		ownerRefs[i].Controller = ptr.To(false)
	}
	objB.SetOwnerReferences(ownerRefs)
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

func (m *NativeRevisionMetadata) ownerRefForCompare() metav1.OwnerReference {
	// Validate the owner.
	ro, ok := m.owner.(runtime.Object)
	if !ok {
		panic(fmt.Sprintf("%T is not a runtime.Object, cannot call SetOwnerReference", m.owner))
	}

	// Create a new owner ref.
	gvk, err := apiutil.GVKForObject(ro, m.scheme)
	if err != nil {
		panic(err)
	}

	ref := metav1.OwnerReference{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		UID:        m.owner.GetUID(),
		Name:       m.owner.GetName(),
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
