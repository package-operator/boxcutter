package managedcache

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ErrNonEmptyKindOrVersion is returned when either kind or version of the given object are empty.
var ErrNonEmptyKindOrVersion = errors.New("object must have non-empty kind and version")

func gvkForObject(obj runtime.Object) (schema.GroupVersionKind, error) {
	gvk := obj.GetObjectKind().GroupVersionKind()
	if gvk.Kind == "" || gvk.Version == "" {
		return schema.GroupVersionKind{}, fmt.Errorf("%w: %s", ErrNonEmptyKindOrVersion, gvk)
	}

	return gvk, nil
}
