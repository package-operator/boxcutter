package machinery

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestCreateCollisionError_Error(t *testing.T) {
	t.Parallel()

	desiredObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "testi",
				"namespace": "test",
			},
		},
	}

	tests := []struct {
		name string
		msg  string
		obj  *unstructured.Unstructured
		want string
	}{
		{
			name: "simple error message",
			obj:  desiredObj,
			msg:  "already exists",
			want: "&{map[apiVersion:v1 kind:Secret metadata:map[name:testi namespace:test]]}: already exists",
		},
		{
			name: "detailed error message",
			obj:  desiredObj,
			msg:  "already exists and is owned by another controller",
			want: "&{map[apiVersion:v1 kind:Secret metadata:map[name:testi namespace:test]]}: already exists and is owned by another controller",
		},
		{
			name: "empty error message",
			obj:  desiredObj,
			msg:  "",
			want: "&{map[apiVersion:v1 kind:Secret metadata:map[name:testi namespace:test]]}: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := NewCreateCollisionError(tt.obj, tt.msg)
			got := err.Error()
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.obj, err.Object())
		})
	}
}

func TestCreateCollisionError_Implementation(t *testing.T) {
	t.Parallel()

	desiredObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "testi",
				"namespace": "test",
			},
		},
	}

	err := CreateCollisionError{object: desiredObj, msg: "test error"}

	var _ error = err
}
