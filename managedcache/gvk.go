package managedcache

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// ErrEmptyKindOrVersion is returned when either kind or version of the given object are empty.
var ErrEmptyKindOrVersion = errors.New("object must have non-empty kind and version")

func gvkForObject(scheme *runtime.Scheme, obj runtime.Object) (schema.GroupVersionKind, error) {
	gvk := obj.GetObjectKind().GroupVersionKind()
	if gvk.Empty() {
		var err error

		gvk, err = apiutil.GVKForObject(obj, scheme)
		if err != nil {
			return schema.GroupVersionKind{}, err
		}
	}

	if gvk.Kind == "" || gvk.Version == "" {
		return schema.GroupVersionKind{}, fmt.Errorf("%w: %s", ErrEmptyKindOrVersion, gvk)
	}

	return gvk, nil
}
