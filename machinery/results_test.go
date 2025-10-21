package machinery

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"
	"sigs.k8s.io/structured-merge-diff/v6/typed"

	"pkg.package-operator.run/boxcutter/machinery/types"
)

var (
	resultExampleObj = &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      "testi",
				"namespace": "test",
			},
		},
	}
	failedExampleProbe = map[string]types.Prober{
		types.ProgressProbeType: &probeStub{
			success: false,
			msgs:    []string{"broken: broken"},
		},
	}
)

func TestObjectResultCreated(t *testing.T) {
	t.Parallel()

	or := newObjectResultCreated(resultExampleObj, failedExampleProbe)
	assert.Equal(t, `Object Deployment.apps/v1 test/testi
Action: "Created"
Probes:
- Progress: Failed
  - broken: broken
`, or.String())
}

func TestNormalObjectResult(t *testing.T) {
	t.Parallel()

	or := newNormalObjectResult(
		ActionProgressed, resultExampleObj,
		CompareResult{
			ConflictingMangers: []CompareResultManagedFields{
				{
					Manager: "hans",
					Fields:  fieldpath.NewSet(fieldpath.MakePathOrDie("spec", "image")),
				},
			},
			Comparison: &typed.Comparison{
				Modified: fieldpath.NewSet(
					fieldpath.MakePathOrDie("spec", "image"),
				),
				Removed: fieldpath.NewSet(),
			},
		}, failedExampleProbe)

	assert.Equal(t, `Object Deployment.apps/v1 test/testi
Action: "Progressed"
Probes:
- Progress: Failed
  - broken: broken
Conflicts:
- "hans"
  .spec.image
Comparison:
- Modified:
  .spec.image
`, or.String())
}

type probeStub struct {
	success bool
	msgs    []string
}

func (s *probeStub) Probe(
	_ client.Object,
) (success bool, messages []string) {
	return s.success, s.msgs
}

func TestObjectResultCreated_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		probes map[string]types.Prober
		want   bool
	}{
		{
			name: "success when all probes pass",
			probes: map[string]types.Prober{
				"probe1": &probeStub{success: true, msgs: []string{"ok"}},
				"probe2": &probeStub{success: true, msgs: []string{"good"}},
			},
			want: true,
		},
		{
			name: "failure when one probe fails",
			probes: map[string]types.Prober{
				"probe1": &probeStub{success: true, msgs: []string{"ok"}},
				"probe2": &probeStub{success: false, msgs: []string{"bad"}},
			},
			want: false,
		},
		{
			name: "failure when all probes fail",
			probes: map[string]types.Prober{
				"probe1": &probeStub{success: false, msgs: []string{"bad1"}},
				"probe2": &probeStub{success: false, msgs: []string{"bad2"}},
			},
			want: false,
		},
		{
			name:   "success when no probes",
			probes: map[string]types.Prober{},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := newObjectResultCreated(resultExampleObj, tt.probes)
			assert.Equal(t, tt.want, result.Success())
		})
	}
}

func TestNormalResult_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		probes map[string]types.Prober
		want   bool
	}{
		{
			name: "success when all probes pass",
			probes: map[string]types.Prober{
				"probe1": &probeStub{success: true, msgs: []string{"ok"}},
				"probe2": &probeStub{success: true, msgs: []string{"good"}},
			},
			want: true,
		},
		{
			name: "failure when one probe fails",
			probes: map[string]types.Prober{
				"probe1": &probeStub{success: true, msgs: []string{"ok"}},
				"probe2": &probeStub{success: false, msgs: []string{"bad"}},
			},
			want: false,
		},
		{
			name:   "success when no probes",
			probes: map[string]types.Prober{},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := newNormalObjectResult(
				ActionUpdated, resultExampleObj,
				CompareResult{}, tt.probes)
			assert.Equal(t, tt.want, result.Success())
		})
	}
}

func TestNormalResult_CompareResult(t *testing.T) {
	t.Parallel()

	compareResult := CompareResult{
		ConflictingMangers: []CompareResultManagedFields{
			{
				Manager: "test-manager",
				Fields:  fieldpath.NewSet(fieldpath.MakePathOrDie("spec", "replicas")),
			},
		},
	}

	result := newNormalObjectResult(
		ActionUpdated, resultExampleObj,
		compareResult, map[string]types.Prober{})

	assert.Equal(t, compareResult, result.CompareResult())
}

func TestObjectResultConflict_Success(t *testing.T) {
	t.Parallel()

	ownerRef := &metav1.OwnerReference{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Name:       "conflicting-owner",
		UID:        "test-uid",
	}

	conflict := newObjectResultConflict(resultExampleObj, CompareResult{}, ownerRef, map[string]types.Prober{})
	assert.False(t, conflict.Success())
}

func TestObjectResultConflict_ConflictingOwner(t *testing.T) {
	t.Parallel()

	ownerRef := &metav1.OwnerReference{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Name:       "conflicting-owner",
		UID:        "test-uid",
	}

	conflict := newObjectResultConflict(resultExampleObj, CompareResult{}, ownerRef, map[string]types.Prober{})

	collisionResult, ok := conflict.(ObjectResultCollision)
	assert.True(t, ok)

	gotOwner, hasOwner := collisionResult.ConflictingOwner()
	assert.True(t, hasOwner)
	assert.Equal(t, ownerRef, gotOwner)
}

func TestObjectResultConflict_String(t *testing.T) {
	t.Parallel()

	ownerRef := &metav1.OwnerReference{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Name:       "conflicting-owner",
		UID:        "test-uid",
	}

	conflict := newObjectResultConflict(resultExampleObj, CompareResult{}, ownerRef, map[string]types.Prober{})
	got := conflict.String()

	assert.Contains(t, got, "Object Deployment.apps/v1 test/testi")
	assert.Contains(t, got, "Action: \"Collision\"")
	assert.Contains(t, got, "Conflicting Owner:")
}
