// Package types contains common type definitions for boxcutter machinery.
package types

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Note: RevisionMetadata and RevisionReference interfaces are defined in metadata.go

// ObjectRef holds information to identify an object.
type ObjectRef struct {
	schema.GroupVersionKind
	client.ObjectKey
}

// ToObjectRef returns an ObjectRef object from unstructured.
func ToObjectRef(obj client.Object) ObjectRef {
	return ObjectRef{
		GroupVersionKind: obj.GetObjectKind().GroupVersionKind(),
		ObjectKey:        client.ObjectKeyFromObject(obj),
	}
}

// String returns a string representation.
func (oid ObjectRef) String() string {
	return fmt.Sprintf("%s %s", oid.GroupVersionKind, oid.ObjectKey)
}

// Phase represents a named collection of objects.
type Phase struct {
	// Name of the Phase.
	Name string
	// Objects contained in the phase.
	Objects []unstructured.Unstructured
}

// GetName returns the name of the phase.
func (p *Phase) GetName() string {
	return p.Name
}

// GetObjects returns the objects contained in the phase.
func (p *Phase) GetObjects() []unstructured.Unstructured {
	return p.Objects
}

// NewRevision creates a new Revision instance.
// It is primarily a convenience function to take advantage of type inference.
func NewRevision[T RevisionMetadata](name string, metadata T, revision int64, phases []Phase) *RevisionImpl[T] {
	return &RevisionImpl[T]{
		Name:     name,
		Metadata: metadata,
		Revision: revision,
		Phases:   phases,
	}
}

type RevisionImpl[T RevisionMetadata] struct {
	// Name of the Revision.
	Name string
	// Metadata manages revision ownership metadata of objects.
	Metadata T
	// Revision number.
	Revision int64
	// Ordered list of phases.
	Phases []Phase
}

// GetName returns the name of the revision.
func (r *RevisionImpl[_]) GetName() string {
	return r.Name
}

// GetMetadata returns the revision metadata handler.
func (r *RevisionImpl[_]) GetMetadata() RevisionMetadata {
	return r.Metadata
}

// GetRevisionNumber returns the current revision number.
func (r *RevisionImpl[_]) GetRevisionNumber() int64 {
	return r.Revision
}

// GetPhases returns the phases a revision is made up of.
func (r *RevisionImpl[_]) GetPhases() []Phase {
	return r.Phases
}

var _ Revision = &RevisionImpl[RevisionMetadata]{}

// Revision represents the version of a content collection consisting of phases.
type Revision interface {
	// GetName returns the name of the revision.
	GetName() string

	// GetMetadata returns the revision metadata handler.
	GetMetadata() RevisionMetadata

	// GetRevisionNumber returns the current revision number.
	GetRevisionNumber() int64

	// GetPhases returns the phases a revision is made up of.
	GetPhases() []Phase
}
