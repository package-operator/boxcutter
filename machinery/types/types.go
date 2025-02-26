// Package types contains common type definitions for boxcutter machinery.
package types

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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

// PhaseAccessor represents a collection of objects lifecycled together.
type PhaseAccessor interface {
	// GetName returns the name of the phase.
	GetName() string
	// GetObjects returns the objects managed by the phase.
	GetObjects() []unstructured.Unstructured
}

// RevisionAccessor represents multiple phases at a given point in time.
type RevisionAccessor interface {
	// GetName returns the name of the revision for reporting and logging.
	GetName() string
	// GetClientObject returns the underlying Kubernetes object backing this revision.
	GetClientObject() client.Object
	// GetRevisionNumber returns the revisions "generation" to order revisions over time.
	GetRevisionNumber() int64
	// GetPhases returns the Phases that make up the revision.
	// Phases will get reconciled in-order and torn down in reverse-order.
	GetPhases() []PhaseAccessor
}

// Phase implements the PhaseAccessor interface.
type Phase struct {
	Name    string
	Objects []unstructured.Unstructured
}

// GetName implements the PhaseAccessor interface.
func (p *Phase) GetName() string {
	return p.Name
}

// GetObjects implements the PhaseAccessor interface.
func (p *Phase) GetObjects() []unstructured.Unstructured {
	return p.Objects
}

// Revision implements the RevisionAccessor interface.
type Revision struct {
	Name     string
	Owner    client.Object
	Revision int64
	Phases   []PhaseAccessor
}

// GetName implements the RevisionAccessor interface.
func (r *Revision) GetName() string {
	return r.Name
}

// GetClientObject implements the RevisionAccessor interface.
func (r *Revision) GetClientObject() client.Object {
	return r.Owner
}

// GetRevisionNumber implements the RevisionAccessor interface.
func (r *Revision) GetRevisionNumber() int64 {
	return r.Revision
}

// GetPhases implements the RevisionAccessor interface.
func (r *Revision) GetPhases() []PhaseAccessor {
	return r.Phases
}
