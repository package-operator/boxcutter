package machinery

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/validation"
)

var errTest = errors.New("AAAAAAh")

func TestPhaseEngine_Reconcile(t *testing.T) {
	t.Parallel()

	oe := &objectEngineMock{}
	pv := &phaseValidatorMock{}
	pe := NewPhaseEngine(oe, pv)

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "12345-678",
			Name:      "owner",
			Namespace: "test",
		},
	}

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "testi",
				"namespace": "test",
			},
		},
	}

	var revision int64 = 1

	pv.
		On("Validate", mock.Anything, mock.Anything, mock.Anything).
		Return(nil)
	oe.On("Reconcile", mock.Anything, revision, obj, mock.Anything).
		Return(newObjectResultCreated(obj, types.ObjectReconcileOptions{}), nil)

	_, err := pe.Reconcile(t.Context(), revision, types.NewPhase(
		"test",
		[]client.Object{
			obj,
		},
	), types.WithOwner(owner, nil))
	require.NoError(t, err)
}

func TestPhaseEngine_Reconcile_PreflightViolation(t *testing.T) {
	t.Parallel()

	oe := &objectEngineMock{}
	pv := &phaseValidatorMock{}
	pe := NewPhaseEngine(oe, pv)

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "12345-678",
			Name:      "owner",
			Namespace: "test",
		},
	}

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "testi",
				"namespace": "test",
			},
		},
	}

	var revision int64 = 1

	pv.
		On("Validate", mock.Anything, mock.Anything, mock.Anything).
		Return(validation.PhaseValidationError{})
	oe.On("Reconcile", mock.Anything, owner, revision, obj, mock.Anything).
		Return(newObjectResultCreated(obj, types.ObjectReconcileOptions{}), nil)

	_, err := pe.Reconcile(t.Context(), revision, types.NewPhase(
		"test",
		[]client.Object{
			obj,
		},
	), types.WithOwner(owner, nil))
	require.NoError(t, err)
	oe.AssertNotCalled(
		t, "Reconcile", mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything,
	)
}

func TestPhaseEngine_Teardown(t *testing.T) {
	t.Parallel()

	oe := &objectEngineMock{}
	pv := &phaseValidatorMock{}
	pe := NewPhaseEngine(oe, pv)

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "12345-678",
			Name:      "owner",
			Namespace: "test",
		},
	}

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "testi",
				"namespace": "test",
			},
		},
	}

	var revision int64 = 1

	oe.On("Teardown", mock.Anything, revision, obj, mock.Anything, mock.Anything).
		Return(true, nil)

	deleted, err := pe.Teardown(t.Context(), revision, types.NewPhase(
		"test", []client.Object{
			obj, obj,
		},
	), types.WithOwner(owner, nil))
	require.NoError(t, err)
	assert.True(t, deleted.IsComplete())
	assert.Empty(t, deleted.Waiting())
	assert.Len(t, deleted.Gone(), 2)
}

type objectEngineMock struct {
	mock.Mock
}

func (m *objectEngineMock) Reconcile(
	ctx context.Context,
	revision int64,
	desiredObject Object,
	opts ...types.ObjectReconcileOption,
) (ObjectResult, error) {
	args := m.Called(ctx, revision, desiredObject, opts)

	return args.Get(0).(ObjectResult), args.Error(1)
}

func (m *objectEngineMock) Teardown(
	ctx context.Context,
	revision int64,
	desiredObject Object,
	opts ...types.ObjectTeardownOption,
) (objectDeleted bool, err error) {
	args := m.Called(ctx, revision, desiredObject, opts)

	return args.Bool(0), args.Error(1)
}

type phaseValidatorMock struct {
	mock.Mock
}

func (m *phaseValidatorMock) Validate(
	ctx context.Context,
	phase types.Phase,
	opts ...types.PhaseReconcileOption,
) error {
	args := m.Called(ctx, phase, opts)

	return args.Error(0)
}

func TestPhaseResult(t *testing.T) {
	t.Parallel()
	t.Run("InTransition", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name     string
			pv       *validation.PhaseValidationError
			res      []ObjectResult
			expected bool
		}{
			{
				name: "true - progressed",
				res: []ObjectResult{
					newObjectResultCreated(nil, types.ObjectReconcileOptions{}),
					newObjectResultProgressed(nil, CompareResult{}, types.ObjectReconcileOptions{}),
				},
				expected: true,
			},
			{
				name: "true - conflict",
				res: []ObjectResult{
					newObjectResultCreated(nil, types.ObjectReconcileOptions{}),
					newObjectResultConflict(nil, CompareResult{}, nil, types.ObjectReconcileOptions{}),
				},
				expected: true,
			},
			{
				name: "false - preflight violation",
				pv: &validation.PhaseValidationError{
					PhaseName:  "xxx",
					PhaseError: errTest,
				},
				res:      []ObjectResult{},
				expected: false,
			},
			{
				name:     "false - empty",
				res:      []ObjectResult{},
				expected: false,
			},
			{
				name: "false - created",
				res: []ObjectResult{
					newObjectResultCreated(nil, types.ObjectReconcileOptions{}),
				},
				expected: false,
			},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				t.Parallel()

				pr := &phaseResult{
					objects: test.res,
				}
				assert.Equal(t, test.expected, pr.InTransition())
			})
		}
	})

	t.Run("IsComplete", func(t *testing.T) {
		t.Parallel()

		failedProbeRes := newObjectResultCreated(nil, types.ObjectReconcileOptions{
			Paused: true,
		}).(ObjectResultCreated)

		tests := []struct {
			name     string
			pv       *validation.PhaseValidationError
			res      []ObjectResult
			expected bool
		}{
			{
				name: "true",
				res: []ObjectResult{
					newObjectResultCreated(nil, types.ObjectReconcileOptions{}),
				},
				expected: true,
			},
			{
				name:     "false - preflight violation",
				pv:       &validation.PhaseValidationError{},
				res:      []ObjectResult{},
				expected: false,
			},
			{
				name: "false - conflict",
				res: []ObjectResult{
					newObjectResultCreated(nil, types.ObjectReconcileOptions{}),
					newObjectResultConflict(nil, CompareResult{}, nil, types.ObjectReconcileOptions{}),
				},
				expected: false,
			},
			{
				name: "false - probe fail",
				res: []ObjectResult{
					failedProbeRes,
				},
				expected: false,
			},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				t.Parallel()

				pr := &phaseResult{
					validationError: test.pv,
					objects:         test.res,
				}
				assert.Equal(t, test.expected, pr.IsComplete())
			})
		}
	})
}

func TestPhaseResult_String(t *testing.T) {
	t.Parallel()

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "testi",
				"namespace": "test",
			},
		},
	}

	r := phaseResult{
		name: "phase-1",
		validationError: &validation.PhaseValidationError{
			PhaseName:  "banana",
			PhaseError: errTest,
		},
		objects: []ObjectResult{
			newObjectResultCreated(obj, types.ObjectReconcileOptions{}),
		},
	}

	assert.Equal(t, `Phase "phase-1"
Complete: false
In Transition: false
Validation Errors:
- AAAAAAh
Objects:
- Object Secret.v1 test/testi
  Action: "Created"
`, r.String())
}

func TestPhaseEngine_NewPhaseEngine(t *testing.T) {
	t.Parallel()

	engine := NewPhaseEngine(nil, nil)
	assert.NotNil(t, engine)
}

func TestPhaseTeardownResult_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result *phaseTeardownResult
		want   string
	}{
		{
			name: "with gone and waiting objects",
			result: &phaseTeardownResult{
				name: "test-phase",
				gone: []types.ObjectRef{
					{
						GroupVersionKind: schema.GroupVersionKind{
							Group:   "",
							Version: "v1",
							Kind:    "ConfigMap",
						},
						ObjectKey: client.ObjectKey{
							Name:      "config1",
							Namespace: "test",
						},
					},
				},
				waiting: []types.ObjectRef{
					{
						GroupVersionKind: schema.GroupVersionKind{
							Group:   "",
							Version: "v1",
							Kind:    "Secret",
						},
						ObjectKey: client.ObjectKey{
							Name:      "secret1",
							Namespace: "test",
						},
					},
				},
			},
			want: `Phase "test-phase"
Gone Objects:
- /v1, Kind=ConfigMap test/config1
Waiting Objects:
- /v1, Kind=Secret test/secret1
`,
		},
		{
			name: "with only gone objects",
			result: &phaseTeardownResult{
				name: "test-phase",
				gone: []types.ObjectRef{
					{
						GroupVersionKind: schema.GroupVersionKind{
							Group:   "",
							Version: "v1",
							Kind:    "ConfigMap",
						},
						ObjectKey: client.ObjectKey{
							Name:      "config1",
							Namespace: "test",
						},
					},
				},
			},
			want: `Phase "test-phase"
Gone Objects:
- /v1, Kind=ConfigMap test/config1
`,
		},
		{
			name: "empty phase",
			result: &phaseTeardownResult{
				name: "empty-phase",
			},
			want: `Phase "empty-phase"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.result.String()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPhaseTeardownResult_GetName(t *testing.T) {
	t.Parallel()

	result := &phaseTeardownResult{name: "test-phase"}
	assert.Equal(t, "test-phase", result.GetName())
}

func TestPhaseResult_GetObjects(t *testing.T) {
	t.Parallel()

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "testi",
				"namespace": "test",
			},
		},
	}

	objects := []ObjectResult{
		newObjectResultCreated(obj, types.ObjectReconcileOptions{}),
	}

	result := &phaseResult{
		objects: objects,
	}

	assert.Equal(t, objects, result.GetObjects())
}
