package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func createObjWithUID() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "test",
				"namespace": "default",
			},
		},
	}
	obj.SetUID("123e4567-e89b-12d3-a456-426614174000")

	return obj
}

func createObjWithGeneration() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name": "test",
			},
		},
	}
	obj.SetGeneration(1)

	return obj
}

func createObjWithGenerateName() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"generateName": "test-",
			},
		},
	}

	return obj
}

func createObjWithFinalizers() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name": "test",
			},
		},
	}
	obj.SetFinalizers([]string{"example.com/finalizer"})

	return obj
}

func createObjWithOwnerReferences() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name": "test",
			},
		},
	}
	obj.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: "v1",
			Kind:       "Pod",
			Name:       "owner",
			UID:        "123",
		},
	})

	return obj
}

func createObjWithResourceVersion() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name": "test",
			},
		},
	}
	obj.SetResourceVersion("12345")

	return obj
}

func createObjWithMultipleForbiddenFields() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name": "test",
			},
		},
	}
	obj.SetUID("123e4567-e89b-12d3-a456-426614174000")
	obj.SetGeneration(1)
	obj.SetResourceVersion("12345")
	obj.SetFinalizers([]string{"example.com/finalizer"})

	return obj
}

func TestValidateObjectMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		obj      *unstructured.Unstructured
		expected []error
	}{
		{
			name: "valid object",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
				},
			},
			expected: nil,
		},
		{
			name: "missing apiVersion",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind": "ConfigMap",
					"metadata": map[string]interface{}{
						"name": "test",
					},
				},
			},
			expected: []error{
				field.Required(field.NewPath("apiVersion"), "must not be empty"),
			},
		},
		{
			name: "missing kind",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"metadata": map[string]interface{}{
						"name": "test",
					},
				},
			},
			expected: []error{
				field.Required(field.NewPath("kind"), "must not be empty"),
			},
		},
		{
			name: "both apiVersion and kind missing",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name": "test",
					},
				},
			},
			expected: []error{
				field.Required(field.NewPath("apiVersion"), "must not be empty"),
				field.Required(field.NewPath("kind"), "must not be empty"),
			},
		},
		{
			name: "uid present",
			obj:  createObjWithUID(),
			expected: []error{
				field.Forbidden(field.NewPath("metadata", "uid"), "must be empty"),
			},
		},
		{
			name: "generation present",
			obj:  createObjWithGeneration(),
			expected: []error{
				field.Forbidden(field.NewPath("metadata", "generation"), "must be empty"),
			},
		},
		{
			name: "generateName present",
			obj:  createObjWithGenerateName(),
			expected: []error{
				field.Forbidden(field.NewPath("metadata", "generateName"), "must be empty"),
			},
		},
		{
			name: "finalizers present",
			obj:  createObjWithFinalizers(),
			expected: []error{
				field.Forbidden(field.NewPath("metadata", "finalizers"), "must be empty"),
			},
		},
		{
			name: "ownerReferences present",
			obj:  createObjWithOwnerReferences(),
			expected: []error{
				field.Forbidden(field.NewPath("metadata", "ownerReferences"), "must be empty"),
			},
		},
		{
			name: "resourceVersion present",
			obj:  createObjWithResourceVersion(),
			expected: []error{
				field.Forbidden(field.NewPath("metadata", "resourceVersion"), "must be empty"),
			},
		},
		{
			name: "multiple forbidden fields",
			obj:  createObjWithMultipleForbiddenFields(),
			expected: []error{
				field.Forbidden(field.NewPath("metadata", "uid"), "must be empty"),
				field.Forbidden(field.NewPath("metadata", "generation"), "must be empty"),
				field.Forbidden(field.NewPath("metadata", "finalizers"), "must be empty"),
				field.Forbidden(field.NewPath("metadata", "resourceVersion"), "must be empty"),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			errors := validateObjectMetadata(test.obj)

			if test.expected == nil {
				assert.Empty(t, errors)
			} else {
				assert.Len(t, errors, len(test.expected))

				for i, expectedErr := range test.expected {
					assert.Equal(t, expectedErr.Error(), errors[i].Error())
				}
			}
		})
	}
}
