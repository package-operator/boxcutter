package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestToObjectRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		obj  client.Object
		want ObjectRef
	}{
		{
			name: "ConfigMap object",
			obj: &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm",
					Namespace: "test-ns",
				},
			},
			want: ObjectRef{
				GroupVersionKind: schema.GroupVersionKind{
					Group:   "",
					Version: "v1",
					Kind:    "ConfigMap",
				},
				ObjectKey: client.ObjectKey{
					Name:      "test-cm",
					Namespace: "test-ns",
				},
			},
		},
		{
			name: "Unstructured object",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"name":      "test-deploy",
						"namespace": "test-ns",
					},
				},
			},
			want: ObjectRef{
				GroupVersionKind: schema.GroupVersionKind{
					Group:   "apps",
					Version: "v1",
					Kind:    "Deployment",
				},
				ObjectKey: client.ObjectKey{
					Name:      "test-deploy",
					Namespace: "test-ns",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ToObjectRef(tt.obj)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestObjectRef_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ref  ObjectRef
		want string
	}{
		{
			name: "namespaced object",
			ref: ObjectRef{
				GroupVersionKind: schema.GroupVersionKind{
					Group:   "",
					Version: "v1",
					Kind:    "ConfigMap",
				},
				ObjectKey: client.ObjectKey{
					Name:      "test-cm",
					Namespace: "test-ns",
				},
			},
			want: "/v1, Kind=ConfigMap test-ns/test-cm",
		},
		{
			name: "cluster-scoped object",
			ref: ObjectRef{
				GroupVersionKind: schema.GroupVersionKind{
					Group:   "",
					Version: "v1",
					Kind:    "Node",
				},
				ObjectKey: client.ObjectKey{
					Name: "test-node",
				},
			},
			want: "/v1, Kind=Node /test-node",
		},
		{
			name: "custom resource",
			ref: ObjectRef{
				GroupVersionKind: schema.GroupVersionKind{
					Group:   "example.com",
					Version: "v1alpha1",
					Kind:    "CustomResource",
				},
				ObjectKey: client.ObjectKey{
					Name:      "test-cr",
					Namespace: "test-ns",
				},
			},
			want: "example.com/v1alpha1, Kind=CustomResource test-ns/test-cr",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.ref.String()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPhase_GetName(t *testing.T) {
	t.Parallel()

	phase := &Phase{
		Name: "test-phase",
		Objects: []unstructured.Unstructured{
			{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name": "test-cm",
					},
				},
			},
		},
	}

	assert.Equal(t, "test-phase", phase.GetName())
}

func TestPhase_GetObjects(t *testing.T) {
	t.Parallel()

	objects := []unstructured.Unstructured{
		{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "test-cm",
				},
			},
		},
		{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata": map[string]interface{}{
					"name": "test-secret",
				},
			},
		},
	}

	phase := &Phase{
		Name:    "test-phase",
		Objects: objects,
	}

	got := phase.GetObjects()
	assert.Equal(t, objects, got)
	assert.Len(t, got, 2)
}

func TestRevision_GetName(t *testing.T) {
	t.Parallel()

	revision := &Revision{
		Name: "test-revision",
	}

	assert.Equal(t, "test-revision", revision.GetName())
}

func TestRevision_GetOwner(t *testing.T) {
	t.Parallel()

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "owner",
			Namespace: "test",
		},
	}

	revision := &Revision{
		Owner: owner,
	}

	assert.Equal(t, owner, revision.GetOwner())
}

func TestRevision_GetRevisionNumber(t *testing.T) {
	t.Parallel()

	revision := &Revision{
		Revision: 42,
	}

	assert.Equal(t, int64(42), revision.GetRevisionNumber())
}

func TestRevision_GetPhases(t *testing.T) {
	t.Parallel()

	phases := []Phase{
		{
			Name: "phase1",
			Objects: []unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "ConfigMap",
						"metadata": map[string]interface{}{
							"name": "test-cm",
						},
					},
				},
			},
		},
		{
			Name: "phase2",
			Objects: []unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Secret",
						"metadata": map[string]interface{}{
							"name": "test-secret",
						},
					},
				},
			},
		},
	}

	revision := &Revision{
		Phases: phases,
	}

	got := revision.GetPhases()
	assert.Equal(t, phases, got)
	assert.Len(t, got, 2)
}
