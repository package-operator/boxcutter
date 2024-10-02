// Package types contains common type definitions for boxcutter machinery.
package types

import (
	"fmt"

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
