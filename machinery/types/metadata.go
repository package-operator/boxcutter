package types

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RevisionMetadata manages revision ownership metadata of objects.
// Implementations may store ownership information in various ways
// (native ownerReferences, annotations, or external systems).
type RevisionMetadata interface {
	// GetReconcileOptions returns a set of options that will added to any
	// revision reconciliation options.
	GetReconcileOptions() []RevisionReconcileOption

	// GetTeardownOptions returns a set of options that will added to any
	// revision teardown options.
	GetTeardownOptions() []RevisionTeardownOption

	// SetCurrent updates obj to mark this RevisionMetadata as the current (controlling) revision.
	// Returns an error if the object already has a different current revision.
	SetCurrent(obj metav1.Object, opts ...SetCurrentOption) error

	// IsCurrent returns true if this RevisionMetadata is the current (controlling) revision of obj.
	IsCurrent(obj metav1.Object) bool

	// RemoveFrom removes this RevisionMetadata from obj, whether it is the current revision or otherwise.
	RemoveFrom(obj metav1.Object)

	// IsNamespaceAllowed returns true if objects may be created/managed in the namespace of obj.
	// The behavior is determined by the constructor configuration (e.g., same-namespace only vs cross-namespace).
	IsNamespaceAllowed(obj metav1.Object) bool

	// CopyReferences copies all revision metadata from objA to objB except the current revision marker.
	// This is used when taking over control from a previous owner while preserving their watch references.
	CopyReferences(objA, objB metav1.Object)

	// GetCurrent returns a RevisionReference describing the current revision of obj.
	// Returns nil if there is no current revision set.
	GetCurrent(obj metav1.Object) RevisionReference
}

// RevisionReference provides information about a revision owner.
// Implementations should return the underlying type when possible
// (e.g., metav1.OwnerReference for Native/Annotation strategies).
type RevisionReference interface {
	String() string
}

// SetCurrentOption is an option which can be passed to SetCurrent.
type SetCurrentOption func(*SetCurrentOptions)

// SetCurrentOptions is a set of options used internally by SetCurrent.
type SetCurrentOptions struct {
	AllowUpdate bool
}

// WithAllowUpdate permits SetCurrent to update the current revision without
// returning an error if the object already has a different current revision.
func WithAllowUpdate(o *SetCurrentOptions) {
	o.AllowUpdate = true
}
