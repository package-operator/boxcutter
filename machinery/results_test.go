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
	oFailedExampleProbe = types.ObjectReconcileOptions{
		Probes: failedExampleProbe,
	}
	failedExampleProbe = map[string]types.Prober{
		types.ProgressProbeType: &probeStub{
			status: types.ProbeStatusFalse,
			msgs:   []string{"broken: broken"},
		},
	}
)

func TestObjectResultCreated_String(t *testing.T) {
	t.Parallel()

	or := newObjectResultCreated(resultExampleObj, oFailedExampleProbe)
	assert.Equal(t, `Object Deployment.apps/v1 test/testi
Action: "Created"
Probes:
- Progress: Failed
  - broken: broken
`, or.String())
}

func TestNormalObjectResult_String(t *testing.T) {
	t.Parallel()

	t.Run("example 1", func(t *testing.T) {
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
			}, oFailedExampleProbe)

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
	})

	t.Run("example 2", func(t *testing.T) {
		t.Parallel()

		or := newNormalObjectResult(
			ActionUpdated, resultExampleObj,
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
			}, types.ObjectReconcileOptions{
				Paused: true,
				Probes: map[string]types.Prober{
					types.ProgressProbeType: &probeStub{
						status: types.ProbeStatusTrue,
					},
				},
			})

		assert.Equal(t, `Object Deployment.apps/v1 test/testi
Action (PAUSED): "Updated"
Probes:
- Progress: Succeeded
Conflicts:
- "hans"
  .spec.image
Comparison:
- Modified:
  .spec.image
`, or.String())
	})
}

type probeStub struct {
	status types.ProbeStatus
	msgs   []string
}

func (s *probeStub) Probe(
	_ client.Object,
) types.ProbeResult {
	return types.ProbeResult{
		Status:   s.status,
		Messages: s.msgs,
	}
}

type commonResultTestFixture struct {
	name    string
	action  Action
	options types.ObjectReconcileOptions
	want    bool
}

var commonResultTestFixtures = []commonResultTestFixture{
	{
		name:   "complete when progression probe passes",
		action: ActionUpdated,
		options: types.ObjectReconcileOptions{
			Probes: map[string]types.Prober{
				types.ProgressProbeType: &probeStub{status: types.ProbeStatusTrue, msgs: []string{"ok"}},
				"probe2":                &probeStub{status: types.ProbeStatusTrue, msgs: []string{"good"}},
			},
		},
		want: true,
	},
	{
		name:   "incomplete when progression probe fails",
		action: ActionUpdated,
		options: types.ObjectReconcileOptions{
			Probes: map[string]types.Prober{
				"probe1":                &probeStub{status: types.ProbeStatusTrue, msgs: []string{"ok"}},
				types.ProgressProbeType: &probeStub{status: types.ProbeStatusFalse, msgs: []string{"bad"}},
			},
		},
		want: false,
	},
	{
		name:   "complete when no probe defined",
		action: ActionUpdated,
		options: types.ObjectReconcileOptions{
			Probes: map[string]types.Prober{},
		},
		want: true,
	},
}

func TestObjectResultCreated_IsComplete(t *testing.T) {
	t.Parallel()

	tests := append([]commonResultTestFixture{
		// your specific test cases go here.
	}, commonResultTestFixtures...)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := newObjectResultCreated(resultExampleObj, tt.options)
			assert.Equal(t, tt.want, result.IsComplete())
		})
	}
}

func TestNormalResult_IsComplete(t *testing.T) {
	t.Parallel()

	tests := append([]commonResultTestFixture{
		// your specific test cases go here.
		{
			name:   "incomplete when paused and Updated",
			action: ActionUpdated,
			options: types.ObjectReconcileOptions{
				Paused: true,
			},
			want: false,
		},
	}, commonResultTestFixtures...)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := newNormalObjectResult(
				tt.action, resultExampleObj,
				CompareResult{}, tt.options)
			assert.Equal(t, tt.want, result.IsComplete())
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
		compareResult, types.ObjectReconcileOptions{})

	assert.Equal(t, compareResult, result.CompareResult())
}

func TestObjectResultConflict_IsComplete(t *testing.T) {
	t.Parallel()

	ownerRef := &metav1.OwnerReference{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Name:       "conflicting-owner",
		UID:        "test-uid",
	}

	conflict := newObjectResultConflict(resultExampleObj, CompareResult{}, ownerRef, types.ObjectReconcileOptions{})
	assert.False(t, conflict.IsComplete())
}

func TestObjectResultConflict_ConflictingOwner(t *testing.T) {
	t.Parallel()

	ownerRef := &metav1.OwnerReference{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Name:       "conflicting-owner",
		UID:        "test-uid",
	}

	conflict := newObjectResultConflict(resultExampleObj, CompareResult{}, ownerRef, types.ObjectReconcileOptions{})

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

	conflict := newObjectResultConflict(resultExampleObj, CompareResult{}, ownerRef, types.ObjectReconcileOptions{})
	got := conflict.String()

	assert.Contains(t, got, "Object Deployment.apps/v1 test/testi")
	assert.Contains(t, got, "Action: \"Collision\"")
	assert.Contains(t, got, "Conflicting Owner:")
}

func Test_isComplete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		action       Action
		probeResults types.ProbeResultContainer
		options      types.ObjectReconcileOptions
		expected     bool
	}{
		{
			name:   "false when progression probe fails",
			action: ActionUpdated,
			options: types.ObjectReconcileOptions{
				// must be defined in options or probe results will be ignored
				Probes: map[string]types.Prober{types.ProgressProbeType: &probeStub{}},
			},
			probeResults: types.ProbeResultContainer{
				types.ProgressProbeType: types.ProbeResult{Status: types.ProbeStatusFalse},
			},
			expected: false,
		},
		{
			name:   "false when progression probe fails, but other probe succeeds",
			action: ActionUpdated,
			options: types.ObjectReconcileOptions{
				// must be defined in options or probe results will be ignored
				Probes: map[string]types.Prober{types.ProgressProbeType: &probeStub{}},
			},
			probeResults: types.ProbeResultContainer{
				types.ProgressProbeType: types.ProbeResult{Status: types.ProbeStatusFalse},
				"other":                 types.ProbeResult{Status: types.ProbeStatusTrue},
			},
			expected: false,
		},
		{
			name:   "true when progression probe succeeds, but other probe fails",
			action: ActionUpdated,
			probeResults: types.ProbeResultContainer{
				types.ProgressProbeType: types.ProbeResult{Status: types.ProbeStatusTrue},
				"other":                 types.ProbeResult{Status: types.ProbeStatusFalse},
			},
			expected: true,
		},
		{
			name:     "false on collision",
			action:   ActionCollision,
			expected: false,
		},
		{
			name:     "false on paused update",
			action:   ActionUpdated,
			options:  types.ObjectReconcileOptions{Paused: true},
			expected: false,
		},
		{
			name:     "false on paused create",
			action:   ActionCreated,
			options:  types.ObjectReconcileOptions{Paused: true},
			expected: false,
		},
		{
			name:     "true on paused progressed - no probes",
			action:   ActionProgressed,
			options:  types.ObjectReconcileOptions{Paused: true},
			expected: true,
		},
		{
			name:     "true on paused idle - no probes",
			action:   ActionIdle,
			options:  types.ObjectReconcileOptions{Paused: true},
			expected: true,
		},

		{
			name:   "false on paused progressed - unknown progress probe",
			action: ActionProgressed,
			options: types.ObjectReconcileOptions{
				Paused: true,
				// must be defined in options or probe results will be ignored
				Probes: map[string]types.Prober{types.ProgressProbeType: &probeStub{}},
			},
			expected: false,
		},
		{
			name:   "true on paused idle - failing progress probe",
			action: ActionIdle,
			options: types.ObjectReconcileOptions{
				Paused: true,
				// must be defined in options or probe results will be ignored
				Probes: map[string]types.Prober{types.ProgressProbeType: &probeStub{}},
			},
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			r := isComplete(test.action, test.probeResults, test.options)
			assert.Equal(t, test.expected, r)
		})
	}
}

func TestObjectResultCollision_Success(t *testing.T) {
	t.Parallel()

	ownerRef := &metav1.OwnerReference{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Name:       "conflicting-owner",
		UID:        "uid-123",
	}

	collision := newObjectResultConflict(
		resultExampleObj,
		CompareResult{},
		ownerRef,
		types.ObjectReconcileOptions{},
	)

	// Cast to access Success() method
	collisionResult, ok := collision.(ObjectResultCollision)
	assert.True(t, ok)

	// Collisions always report as not successful
	assert.False(t, collisionResult.Success())
}

func TestNewNormalObjectResult_PanicOnCreated(t *testing.T) {
	t.Parallel()

	// newNormalObjectResult should panic when ActionCreated is passed
	assert.Panics(t, func() {
		newNormalObjectResult(
			ActionCreated,
			resultExampleObj,
			CompareResult{},
			types.ObjectReconcileOptions{},
		)
	})
}
