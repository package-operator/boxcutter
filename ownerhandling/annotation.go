package ownerhandling

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	bctypes "pkg.package-operator.run/boxcutter/machinery/types"
)

// Ensure AnnotationRevisionMetadata implements RevisionMetadata.
var _ bctypes.RevisionMetadata = (*annotationRevisionMetadata)(nil)

func NewAnnotationHandler(annotationKey string) annotationHandler {
	return annotationHandler{annotationKey: annotationKey}
}

type annotationHandler struct {
	annotationKey string
}

// annotationRevisionMetadata uses annotations for cross-namespace ownership tracking.
// Cross-namespace is always allowed (this is the primary purpose of annotation-based ownership).
type annotationRevisionMetadata struct {
	owner         client.Object
	scheme        *runtime.Scheme
	annotationKey string
}

// NewRevisionMetadata creates a RevisionMetadata using annotation-based ownership.
// IsNamespaceAllowed() always returns true since cross-namespace support is the primary
// purpose of annotation-based ownership.
// Panics if owner has an empty UID (not persisted to cluster).
func (h *annotationHandler) NewRevisionMetadata(
	owner client.Object,
	scheme *runtime.Scheme,
) *annotationRevisionMetadata {
	if len(owner.GetUID()) == 0 {
		panic("owner must be persisted to cluster, empty UID")
	}

	return &annotationRevisionMetadata{
		owner:         owner,
		scheme:        scheme,
		annotationKey: h.annotationKey,
	}
}

// SetCurrent updates obj to mark this RevisionMetadata as the current (controlling) revision.
// Returns an error if the object already has a different current revision.
func (m *annotationRevisionMetadata) SetCurrent(obj metav1.Object) error {
	ownerRefComp := m.ownerRefForCompare()
	ownerRefs := m.getOwnerReferences(obj)

	// Ensure that there is no controller already.
	for _, ownerRef := range ownerRefs {
		if !referSameObject(ownerRefComp, ownerRef) &&
			ownerRef.Controller != nil && *ownerRef.Controller {
			return &controllerutil.AlreadyOwnedError{
				Object: obj,
				Owner: metav1.OwnerReference{
					APIVersion: ownerRef.APIVersion,
					Kind:       ownerRef.Kind,
					Name:       ownerRef.Name,
					Controller: ownerRef.Controller,
					UID:        ownerRef.UID,
				},
			}
		}
	}

	gvk, err := apiutil.GVKForObject(m.owner.(runtime.Object), m.scheme)
	if err != nil {
		return err
	}

	ownerRef := annotationOwnerRef{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		UID:        m.owner.GetUID(),
		Name:       m.owner.GetName(),
		Namespace:  m.owner.GetNamespace(),
		Controller: ptr.To(true),
	}

	ownerIndex := slices.IndexFunc(ownerRefs, func(ref annotationOwnerRef) bool {
		return referSameObject(ownerRef, ref)
	})
	if ownerIndex != -1 {
		ownerRefs[ownerIndex] = ownerRef
	} else {
		ownerRefs = append(ownerRefs, ownerRef)
	}

	m.setOwnerReferences(obj, ownerRefs)

	return nil
}

// IsCurrent returns true if this RevisionMetadata is the current (controlling) revision of obj.
func (m *annotationRevisionMetadata) IsCurrent(obj metav1.Object) bool {
	ownerRefComp := m.ownerRefForCompare()
	for _, ownerRef := range m.getOwnerReferences(obj) {
		if referSameObject(ownerRefComp, ownerRef) &&
			ownerRef.Controller != nil &&
			*ownerRef.Controller {
			return true
		}
	}

	return false
}

// RemoveFrom removes this RevisionMetadata from obj, whether it is the current revision or otherwise.
func (m *annotationRevisionMetadata) RemoveFrom(obj metav1.Object) {
	ownerRefComp := m.ownerRefForCompare()
	ownerRefs := m.getOwnerReferences(obj)
	foundIndex := -1

	for i, ownerRef := range ownerRefs {
		if referSameObject(ownerRefComp, ownerRef) {
			foundIndex = i

			break
		}
	}

	if foundIndex != -1 {
		m.setOwnerReferences(obj, slices.Delete(ownerRefs, foundIndex, foundIndex+1))
	}
}

// IsNamespaceAllowed returns true if objects may be created/managed in the namespace of obj.
// For annotation-based ownership, cross-namespace is always allowed.
func (m *annotationRevisionMetadata) IsNamespaceAllowed(_ metav1.Object) bool {
	return true
}

// CopyReferences copies all revision metadata from objA to objB except the current revision marker.
// This is used when taking over control from a previous owner while preserving their watch references.
func (m *annotationRevisionMetadata) CopyReferences(objA, objB metav1.Object) {
	// Copy owner references from A to B.
	ownerRefs := m.getOwnerReferences(objA)
	// Release controller (set all Controller fields to nil/false).
	for i := range ownerRefs {
		ownerRefs[i].Controller = nil
	}
	m.setOwnerReferences(objB, ownerRefs)
}

// GetCurrent returns a RevisionReference describing the current revision of obj.
// Returns nil if there is no current revision set.
func (m *annotationRevisionMetadata) GetCurrent(obj metav1.Object) bctypes.RevisionReference {
	for _, ref := range m.getOwnerReferences(obj) {
		if ref.Controller != nil && *ref.Controller {
			// Convert to metav1.OwnerReference for consistent return type.
			ownerRef := ref.ToMetaV1OwnerRef()

			return &ownerRef
		}
	}

	return nil
}

func (m *annotationRevisionMetadata) getOwnerReferences(obj metav1.Object) []annotationOwnerRef {
	return getAnnotationOwnerReferences(obj, m.annotationKey)
}

// getAnnotationOwnerReferences returns the owner references stored in the given annotation key.
func getAnnotationOwnerReferences(obj metav1.Object, annotationKey string) []annotationOwnerRef {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return nil
	}

	if len(annotations[annotationKey]) == 0 {
		return nil
	}

	var ownerReferences []annotationOwnerRef
	if err := json.Unmarshal([]byte(annotations[annotationKey]), &ownerReferences); err != nil {
		panic(err)
	}

	return ownerReferences
}

func (m *annotationRevisionMetadata) setOwnerReferences(obj metav1.Object, owners []annotationOwnerRef) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	j, err := json.Marshal(owners)
	if err != nil {
		panic(err)
	}

	annotations[m.annotationKey] = string(j)
	obj.SetAnnotations(annotations)
}

func (m *annotationRevisionMetadata) ownerRefForCompare() annotationOwnerRef {
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

	ref := annotationOwnerRef{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		UID:        m.owner.GetUID(),
		Name:       m.owner.GetName(),
	}

	return ref
}

func referSameObject(a, b annotationOwnerRef) bool {
	aGV, err := schema.ParseGroupVersion(a.APIVersion)
	if err != nil {
		return false
	}

	bGV, err := schema.ParseGroupVersion(b.APIVersion)
	if err != nil {
		return false
	}

	return aGV.Group == bGV.Group && a.Kind == b.Kind && a.Name == b.Name && a.UID == b.UID
}

// annotationOwnerRef represents an owner reference stored in annotations.
// This is used for cross-namespace ownership tracking where native ownerReferences cannot be used.
type annotationOwnerRef struct {
	// API version of the referent.
	APIVersion string `json:"apiVersion"`
	// Kind of the referent.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	Kind string `json:"kind"`
	// Name of the referent.
	// More info: http://kubernetes.io/docs/user-guide/identifiers#names
	Name string `json:"name"`
	// Namespace of the referent.
	// More info: http://kubernetes.io/docs/user-guide/identifiers#namespaces
	Namespace string `json:"namespace"`
	// UID of the referent.
	// More info: http://kubernetes.io/docs/user-guide/identifiers#uids
	UID types.UID `json:"uid"`
	// If true, this reference struct points to the managing controller.
	// +optional
	Controller *bool `json:"controller,omitempty"`
}

func (r *annotationOwnerRef) isController() bool {
	return r.Controller != nil && *r.Controller
}

func (r *annotationOwnerRef) ToMetaV1OwnerRef() metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion:         r.APIVersion,
		Kind:               r.Kind,
		Name:               r.Name,
		UID:                r.UID,
		Controller:         r.Controller,
		BlockOwnerDeletion: ptr.To(true),
	}
}

// AnnotationEnqueueRequestForOwner returns an EventHandler that enqueues reconcile requests
// for the owner of the object that triggered the event, using annotation-based owner references.
func (h *annotationHandler) EnqueueRequestForOwner(
	scheme *runtime.Scheme,
	ownerType client.Object,
	isController bool,
) handler.EventHandler {
	e := &annotationEnqueueRequestForOwner{
		OwnerType:     ownerType,
		IsController:  isController,
		annotationKey: h.annotationKey,
	}
	if err := e.parseOwnerTypeGroupKind(scheme); err != nil {
		panic(err)
	}

	return e
}

// annotationEnqueueRequestForOwner implements an EventHandler using annotation-based owner references.
type annotationEnqueueRequestForOwner struct {
	// OwnerType is the type of the Owner object to look for in OwnerReferences.  Only Group and Kind are compared.
	OwnerType client.Object

	// IsController if set will only look at the first OwnerReference with Controller: true.
	IsController bool

	// annotationKey is the annotation key used to store owner references.
	annotationKey string

	// ownerGK is the cached Group and Kind for the OwnerType.
	ownerGK schema.GroupKind
}

var _ handler.EventHandler = (*annotationEnqueueRequestForOwner)(nil)

// Create implements EventHandler.
func (e *annotationEnqueueRequestForOwner) Create(
	_ context.Context, evt event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	for _, req := range e.getOwnerReconcileRequest(evt.Object) {
		q.Add(req)
	}
}

// Update implements EventHandler.
func (e *annotationEnqueueRequestForOwner) Update(
	_ context.Context, evt event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	for _, req := range e.getOwnerReconcileRequest(evt.ObjectOld) {
		q.Add(req)
	}

	for _, req := range e.getOwnerReconcileRequest(evt.ObjectNew) {
		q.Add(req)
	}
}

// Delete implements EventHandler.
func (e *annotationEnqueueRequestForOwner) Delete(
	_ context.Context, evt event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	for _, req := range e.getOwnerReconcileRequest(evt.Object) {
		q.Add(req)
	}
}

// Generic implements EventHandler.
func (e *annotationEnqueueRequestForOwner) Generic(
	_ context.Context, evt event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	for _, req := range e.getOwnerReconcileRequest(evt.Object) {
		q.Add(req)
	}
}

func (e *annotationEnqueueRequestForOwner) getOwnerReconcileRequest(object metav1.Object) []reconcile.Request {
	ownerReferences := getAnnotationOwnerReferences(object, e.annotationKey)
	requests := make([]reconcile.Request, 0, len(ownerReferences))

	for _, ownerRef := range ownerReferences {
		ownerRefGV, err := schema.ParseGroupVersion(ownerRef.APIVersion)
		if err != nil {
			return nil
		}

		if ownerRefGV.Group != e.ownerGK.Group ||
			ownerRef.Kind != e.ownerGK.Kind {
			continue
		}

		if e.IsController && !ownerRef.isController() {
			continue
		}

		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKey{
				Name:      ownerRef.Name,
				Namespace: ownerRef.Namespace,
			},
		})
	}

	return requests
}

// ErrMultipleKinds is returned when an object matches multiple kind registered in scheme.
var ErrMultipleKinds = errors.New("multiple kinds error: expected exactly one kind")

// parseOwnerTypeGroupKind parses the OwnerType into a Group and Kind and caches the result.
func (e *annotationEnqueueRequestForOwner) parseOwnerTypeGroupKind(scheme *runtime.Scheme) error {
	// Get the kinds of the type
	kinds, _, err := scheme.ObjectKinds(e.OwnerType)
	if err != nil {
		return err
	}
	// Expect only 1 kind.  If there is more than one kind this is probably an edge case such as ListOptions.
	if len(kinds) != 1 {
		return fmt.Errorf("%w. For ownerType %T, found %s kinds", ErrMultipleKinds, e.OwnerType, kinds)
	}
	// Cache the Group and Kind for the OwnerType
	e.ownerGK = schema.GroupKind{Group: kinds[0].Group, Kind: kinds[0].Kind}

	return nil
}
