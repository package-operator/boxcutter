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
		{
			name: "nil object",
			obj:  nil,
			want: ObjectRef{},
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

	phase := NewPhase("test-phase", []client.Object{
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "test-cm",
				},
			},
		},
	})

	assert.Equal(t, "test-phase", phase.GetName())
}

func TestPhase_GetObjects(t *testing.T) {
	t.Parallel()

	objects := []client.Object{
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "test-cm",
				},
			},
		},
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata": map[string]interface{}{
					"name": "test-secret",
				},
			},
		},
	}

	phase := NewPhase(
		"test-phase", objects,
	)

	got := phase.GetObjects()
	assert.Equal(t, objects, got)
	assert.Len(t, got, 2)
}

func TestRevision_GetName(t *testing.T) {
	t.Parallel()

	revision := NewRevision(
		"test-revision", 1, nil,
	)

	assert.Equal(t, "test-revision", revision.GetName())
}

func TestRevision_GetRevisionNumber(t *testing.T) {
	t.Parallel()

	revision := NewRevision("test-revision", 42, nil)

	assert.Equal(t, int64(42), revision.GetRevisionNumber())
}

func TestRevision_GetPhases(t *testing.T) {
	t.Parallel()

	phases := []Phase{
		&phase{
			Name: "phase1",
			Objects: []client.Object{
				&unstructured.Unstructured{
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
		&phase{
			Name: "phase2",
			Objects: []client.Object{
				&unstructured.Unstructured{
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

	revision := NewRevision(
		"test", 2, phases,
	)

	got := revision.GetPhases()
	assert.Equal(t, phases, got)
	assert.Len(t, got, 2)
}

func TestPhase_WithReconcileOptions(t *testing.T) {
	t.Parallel()

	phase := NewPhase("test-phase", []client.Object{})

	opts := []PhaseReconcileOption{
		WithCollisionProtection(CollisionProtectionNone),
	}

	result := phase.WithReconcileOptions(opts...)

	assert.Equal(t, phase, result, "should return same phase for chaining")
	assert.Equal(t, opts, phase.GetReconcileOptions())
}

func TestPhase_WithTeardownOptions(t *testing.T) {
	t.Parallel()

	phase := NewPhase("test-phase", []client.Object{})

	opts := []PhaseTeardownOption{}

	result := phase.WithTeardownOptions(opts...)

	assert.Equal(t, phase, result, "should return same phase for chaining")
	assert.Empty(t, phase.GetTeardownOptions())

	chainedOpts := []PhaseTeardownOption{
		WithTeardownWriter(nil),
	}

	result = phase.WithTeardownOptions(chainedOpts...)
	assert.Equal(t, chainedOpts, result.GetTeardownOptions())
}

func TestPhase_GetReconcileOptions(t *testing.T) {
	t.Parallel()

	opts := []PhaseReconcileOption{
		WithCollisionProtection(CollisionProtectionNone),
	}

	phase := NewPhase("test-phase", []client.Object{}).
		WithReconcileOptions(opts...)

	assert.Equal(t, opts, phase.GetReconcileOptions())
}

func TestPhase_GetTeardownOptions(t *testing.T) {
	t.Parallel()

	opts := []PhaseTeardownOption{}

	phase := NewPhase("test-phase", []client.Object{}).
		WithTeardownOptions(opts...)

	assert.Empty(t, phase.GetTeardownOptions())
}

func TestNewPhaseWithOwner(t *testing.T) {
	t.Parallel()

	owner := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "owner",
			Namespace: "test-ns",
			UID:       "test-uid",
		},
	}

	objects := []client.Object{
		&unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "test-cm",
				},
			},
		},
	}

	mockStrat := &mockOwnerStrategy{}

	phase := NewPhaseWithOwner("test-phase", objects, owner, mockStrat)

	assert.Equal(t, "test-phase", phase.GetName())
	assert.Equal(t, objects, phase.GetObjects())

	reconcileOpts := phase.GetReconcileOptions()
	assert.NotEmpty(t, reconcileOpts)

	teardownOpts := phase.GetTeardownOptions()
	assert.NotEmpty(t, teardownOpts)
}

func TestRevision_WithReconcileOptions(t *testing.T) {
	t.Parallel()

	revision := NewRevision("test-revision", 1, nil)

	opts := []RevisionReconcileOption{
		WithCollisionProtection(CollisionProtectionNone),
	}

	result := revision.WithReconcileOptions(opts...)

	assert.Equal(t, revision, result, "should return same revision for chaining")
	assert.Equal(t, opts, revision.GetReconcileOptions())
}

func TestRevision_WithTeardownOptions(t *testing.T) {
	t.Parallel()

	revision := NewRevision("test-revision", 1, nil)

	opts := []RevisionTeardownOption{}

	result := revision.WithTeardownOptions(opts...)

	assert.Equal(t, revision, result, "should return same revision for chaining")
	assert.Equal(t, opts, revision.GetTeardownOptions())
}

func TestRevision_GetReconcileOptions(t *testing.T) {
	t.Parallel()

	opts := []RevisionReconcileOption{
		WithCollisionProtection(CollisionProtectionNone),
	}

	revision := NewRevision("test-revision", 1, nil).
		WithReconcileOptions(opts...)

	assert.Equal(t, opts, revision.GetReconcileOptions())
}

func TestRevision_GetTeardownOptions(t *testing.T) {
	t.Parallel()

	opts := []RevisionTeardownOption{}

	revision := NewRevision("test-revision", 1, nil).
		WithTeardownOptions(opts...)

	assert.Equal(t, opts, revision.GetTeardownOptions())
}

func TestNewRevisionWithOwner(t *testing.T) {
	t.Parallel()

	owner := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "owner",
			Namespace: "test-ns",
			UID:       "test-uid",
		},
	}

	phases := []Phase{
		NewPhase("phase1", []client.Object{}),
	}

	mockStrat := &mockOwnerStrategy{}

	revision := NewRevisionWithOwner("test-revision", 1, phases, owner, mockStrat)

	assert.Equal(t, "test-revision", revision.GetName())
	assert.Equal(t, int64(1), revision.GetRevisionNumber())
	assert.Equal(t, phases, revision.GetPhases())

	reconcileOpts := revision.GetReconcileOptions()
	assert.NotEmpty(t, reconcileOpts)

	teardownOpts := revision.GetTeardownOptions()
	assert.NotEmpty(t, teardownOpts)
}
