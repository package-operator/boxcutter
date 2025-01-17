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

// Phase represents a collection of objects lifecycled together.
type Phase interface {
	// GetName returns the name of the phase.
	GetName() string
	// GetObjects returns the objects managed by the phase.
	GetObjects() []PhaseObject
}

// PhaseObject represents an object and it's options.
type PhaseObject struct {
	Object *unstructured.Unstructured
	Opts   []ObjectOption
}

// Revision represents multiple phases at a given point in time.
type Revision interface {
	// GetName returns the name of the revision for reporting and logging.
	GetName() string
	// GetClientObject returns the underlying Kubernetes object backing this revision.
	GetClientObject() client.Object
	// GetRevisionNumber returns the revisions "generation" to order revisions over time.
	GetRevisionNumber() int64
	// GetPhases returns the Phases that make up the revision.
	// Phases will get reconciled in-order and torn down in reverse-order.
	GetPhases() []Phase
}
