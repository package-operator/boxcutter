//go:build integration

package boxcutter

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func cleanupOnSuccess(t *testing.T, obj client.Object) {
	t.Helper()
	t.Cleanup(func() {
		if !t.Failed() {
			// Make sure objects are completely gone before closing the test.
			//nolint:usetesting
			ctx := context.Background()
			_ = Client.Delete(ctx, obj, client.PropagationPolicy(metav1.DeletePropagationForeground))
			_ = Waiter.WaitToBeGone(ctx, obj, func(client.Object) (bool, error) { return false, nil })
		}
	})
}

// Retrieves the GVK of the given client.Object from `Scheme`.
// Panics if this fails.
func mustGVKForObject(obj client.Object) schema.GroupVersionKind {
	gvk, err := apiutil.GVKForObject(obj, Scheme)
	must(err)

	return gvk
}

// Converts the given client.Object to an unstructured object.
// Panics if this fails.
func toUns(obj client.Object) unstructured.Unstructured {
	must(setTypeMeta(obj, Scheme))
	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	must(err)

	return unstructured.Unstructured{
		Object: raw,
	}
}

// Needed for `setTypeMeta`.
var typeMetaType = reflect.TypeOf(metav1.TypeMeta{})

// Test helper that uses reflection to get to the underlying struct value of a `runtime.Object`
// and set its TypeMeta field with data acquired from the passed scheme.
// Trivia:
// `runtime.Object` is a narrower `client.Object`, so this will work on `client.Object`s, too.
//
//nolint:err113
func setTypeMeta(o runtime.Object, scheme *runtime.Scheme) error {
	// Get value from interface.
	value := reflect.ValueOf(o)

	// Dereference pointers.
	for value.Kind() == reflect.Pointer {
		value = value.Elem()
	}

	// Ensure that a field called "TypeMeta" exists
	// and that it has the correct type.
	fieldType, fieldFound := value.Type().FieldByName("TypeMeta")
	if !fieldFound {
		return errors.New("TypeMeta field is missing on input value")
	}

	if !typeMetaType.AssignableTo(fieldType.Type) {
		return fmt.Errorf("field is having wrong type: %s", fieldType.Type)
	}

	// Prepare TypeMeta value.
	gvk, err := apiutil.GVKForObject(o.(client.Object), scheme)
	if err != nil {
		return err
	}

	typeMeta := metav1.TypeMeta{
		Kind:       gvk.Kind,
		APIVersion: gvk.GroupVersion().String(),
	}

	// Set value to field.
	field := value.FieldByName("TypeMeta")
	field.Set(reflect.ValueOf(typeMeta))

	return nil
}

func newConfigMap(name string, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      name,
		},
		Data: data,
	}
}
