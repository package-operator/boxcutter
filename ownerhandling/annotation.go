package ownerhandling

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
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
)

var _ ownerStrategy = (*OwnerStrategyAnnotation)(nil)

// OwnerStrategyAnnotation handling strategy uses .metadata.annotations.
// Allows cross-namespace owner references.
type OwnerStrategyAnnotation struct {
	scheme        *runtime.Scheme
	annotationKey string
}

// NewAnnotation returns a new OwnerStrategyAnnotation instance.
func NewAnnotation(scheme *runtime.Scheme, annotationKey string) *OwnerStrategyAnnotation {
	return &OwnerStrategyAnnotation{
		scheme:        scheme,
		annotationKey: annotationKey,
	}
}

// GetController returns the OwnerReference with Controller==true, if one exist.
func (s *OwnerStrategyAnnotation) GetController(obj metav1.Object) (
	metav1.OwnerReference, bool,
) {
	for _, ref := range s.getOwnerReferences(obj) {
		if ref.Controller != nil && *ref.Controller {
			return ref.ToMetaV1OwnerRef(), true
		}
	}

	return metav1.OwnerReference{}, false
}

// CopyOwnerReferences copies all OwnerReferences from objA to objB,
// overriding any existing OwnerReferences on objB.
func (s *OwnerStrategyAnnotation) CopyOwnerReferences(objA, objB metav1.Object) {
	s.setOwnerReferences(objB, s.getOwnerReferences(objA))
}

// EnqueueRequestForOwner returns a EventHandler to enqueue the owner.
func (s *OwnerStrategyAnnotation) EnqueueRequestForOwner(
	ownerType client.Object, _ meta.RESTMapper, isController bool,
) handler.EventHandler {
	a := &AnnotationEnqueueRequestForOwner{
		OwnerType:     ownerType,
		IsController:  isController,
		ownerStrategy: s,
	}
	if err := a.parseOwnerTypeGroupKind(s.scheme); err != nil {
		// This (passing a type that is not in the scheme) HAS to be a
		// programmer error and can't be recovered at runtime anyways.
		panic(err)
	}

	return a
}

// SetOwnerReference adds owner as OwnerReference to obj, with Controller set to false.
func (s *OwnerStrategyAnnotation) SetOwnerReference(owner, obj metav1.Object) error {
	ownerRefs := s.getOwnerReferences(obj)

	gvk, err := apiutil.GVKForObject(owner.(runtime.Object), s.scheme)
	if err != nil {
		return err
	}

	ownerRef := annotationOwnerRef{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		UID:        owner.GetUID(),
		Name:       owner.GetName(),
		Namespace:  owner.GetNamespace(),
	}

	ownerIndex := s.indexOf(ownerRefs, ownerRef)
	if ownerIndex != -1 {
		ownerRefs[ownerIndex] = ownerRef
	} else {
		ownerRefs = append(ownerRefs, ownerRef)
	}

	s.setOwnerReferences(obj, ownerRefs)

	return nil
}

// SetControllerReference adds owner as OwnerReference to obj, with Controller set to true.
func (s *OwnerStrategyAnnotation) SetControllerReference(owner, obj metav1.Object) error {
	ownerRefComp := s.ownerRefForCompare(owner)
	ownerRefs := s.getOwnerReferences(obj)

	// Ensure that there is no controller already.
	for _, ownerRef := range ownerRefs {
		if !s.referSameObject(ownerRefComp, ownerRef) &&
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

	gvk, err := apiutil.GVKForObject(owner.(runtime.Object), s.scheme)
	if err != nil {
		return err
	}

	ownerRef := annotationOwnerRef{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		UID:        owner.GetUID(),
		Name:       owner.GetName(),
		Namespace:  owner.GetNamespace(),
		Controller: ptr.To(true),
	}

	ownerIndex := s.indexOf(ownerRefs, ownerRef)
	if ownerIndex != -1 {
		ownerRefs[ownerIndex] = ownerRef
	} else {
		ownerRefs = append(ownerRefs, ownerRef)
	}

	s.setOwnerReferences(obj, ownerRefs)

	return nil
}

// IsOwner returns true if owner is contained in object OwnerReference list.
func (s *OwnerStrategyAnnotation) IsOwner(owner, obj metav1.Object) bool {
	ownerRefComp := s.ownerRefForCompare(owner)
	for _, ownerRef := range s.getOwnerReferences(obj) {
		if s.referSameObject(ownerRefComp, ownerRef) {
			return true
		}
	}

	return false
}

// IsController returns true if the given owner is the controller of obj.
func (s *OwnerStrategyAnnotation) IsController(
	owner, obj metav1.Object,
) bool {
	ownerRefComp := s.ownerRefForCompare(owner)
	for _, ownerRef := range s.getOwnerReferences(obj) {
		if s.referSameObject(ownerRefComp, ownerRef) &&
			ownerRef.Controller != nil &&
			*ownerRef.Controller {
			return true
		}
	}

	return false
}

// RemoveOwner removes the owner from objs OwnerReference list.
func (s *OwnerStrategyAnnotation) RemoveOwner(owner, obj metav1.Object) {
	ownerRefComp := s.ownerRefForCompare(owner)
	ownerRefs := s.getOwnerReferences(obj)
	foundIndex := -1

	for i, ownerRef := range ownerRefs {
		if s.referSameObject(ownerRefComp, ownerRef) {
			// remove owner
			foundIndex = i

			break
		}
	}

	if foundIndex != -1 {
		s.setOwnerReferences(obj, remove(ownerRefs, foundIndex))
	}
}

// ReleaseController sets all OwnerReferences Controller to false.
func (s *OwnerStrategyAnnotation) ReleaseController(obj metav1.Object) {
	ownerRefs := s.getOwnerReferences(obj)
	for i := range ownerRefs {
		ownerRefs[i].Controller = nil
	}

	s.setOwnerReferences(obj, ownerRefs)
}

func (s *OwnerStrategyAnnotation) getOwnerReferences(obj metav1.Object) []annotationOwnerRef {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return nil
	}

	if len(annotations[s.annotationKey]) == 0 {
		return nil
	}

	var ownerReferences []annotationOwnerRef
	if err := json.Unmarshal([]byte(annotations[s.annotationKey]), &ownerReferences); err != nil {
		panic(err)
	}

	return ownerReferences
}

func (s *OwnerStrategyAnnotation) setOwnerReferences(obj metav1.Object, owners []annotationOwnerRef) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	j, err := json.Marshal(owners)
	if err != nil {
		panic(err)
	}

	annotations[s.annotationKey] = string(j)
	obj.SetAnnotations(annotations)
}

func (s *OwnerStrategyAnnotation) indexOf(ownerRefs []annotationOwnerRef, ownerRef annotationOwnerRef) int {
	for i := range ownerRefs {
		if s.referSameObject(ownerRef, ownerRefs[i]) {
			return i
		}
	}

	return -1
}

func (s *OwnerStrategyAnnotation) ownerRefForCompare(owner metav1.Object) annotationOwnerRef {
	// Validate the owner.
	ro, ok := owner.(runtime.Object)
	if !ok {
		panic(fmt.Sprintf("%T is not a runtime.Object, cannot call SetOwnerReference", owner))
	}

	// Create a new owner ref.
	gvk, err := apiutil.GVKForObject(ro, s.scheme)
	if err != nil {
		panic(err)
	}

	ref := annotationOwnerRef{
		APIVersion: gvk.GroupVersion().String(),
		Kind:       gvk.Kind,
		UID:        owner.GetUID(),
		Name:       owner.GetName(),
	}

	return ref
}

// Returns true if a and b point to the same object.
func (s *OwnerStrategyAnnotation) referSameObject(a, b annotationOwnerRef) bool {
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

type annotationOwnerRef struct {
	// API version of the referent.
	APIVersion string `json:"apiVersion"`
	// Kind of the referent.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	Kind string `json:"kind"`
	// Name of the referent.
	// More info: http://kubernetes.io/docs/user-guide/identifiers#names
	Name string `json:"name"`
	// Name of the referent.
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

// AnnotationEnqueueRequestForOwner implements an EventHandler using the annotation ownerStrategy.
type AnnotationEnqueueRequestForOwner struct {
	// OwnerType is the type of the Owner object to look for in OwnerReferences.  Only Group and Kind are compared.
	OwnerType client.Object

	// IsController if set will only look at the first OwnerReference with Controller: true.
	IsController bool

	// OwnerType is the type of the Owner object to look for in OwnerReferences.  Only Group and Kind are compared.
	ownerGK schema.GroupKind

	ownerStrategy *OwnerStrategyAnnotation
}

// Create implements EventHandler.
func (e *AnnotationEnqueueRequestForOwner) Create(
	_ context.Context, evt event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	for _, req := range e.getOwnerReconcileRequest(evt.Object) {
		q.Add(req)
	}
}

// Update implements EventHandler.
func (e *AnnotationEnqueueRequestForOwner) Update(
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
func (e *AnnotationEnqueueRequestForOwner) Delete(
	_ context.Context, evt event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	for _, req := range e.getOwnerReconcileRequest(evt.Object) {
		q.Add(req)
	}
}

// Generic implements EventHandler.
func (e *AnnotationEnqueueRequestForOwner) Generic(
	_ context.Context, evt event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request],
) {
	for _, req := range e.getOwnerReconcileRequest(evt.Object) {
		q.Add(req)
	}
}

func (e *AnnotationEnqueueRequestForOwner) getOwnerReconcileRequest(object metav1.Object) []reconcile.Request {
	ownerReferences := e.ownerStrategy.getOwnerReferences(object)
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
func (e *AnnotationEnqueueRequestForOwner) parseOwnerTypeGroupKind(scheme *runtime.Scheme) error {
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
