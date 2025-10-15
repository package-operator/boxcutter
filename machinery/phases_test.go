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
	oe.On("Reconcile", mock.Anything, owner, revision, obj, mock.Anything).
		Return(newNormalObjectResult(ActionCreated, obj, CompareResult{}, types.ObjectReconcileOptions{}), nil)

	_, err := pe.Reconcile(t.Context(), owner, revision, types.Phase{
		Name: "test",
		Objects: []unstructured.Unstructured{
			*obj,
		},
	})
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
		Return(newNormalObjectResult(ActionCreated, obj, CompareResult{}, types.ObjectReconcileOptions{}), nil)

	_, err := pe.Reconcile(t.Context(), owner, revision, types.Phase{
		Name: "test",
		Objects: []unstructured.Unstructured{
			*obj,
		},
	})
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

	oe.On("Teardown", mock.Anything, owner, revision, obj, mock.Anything, mock.Anything).
		Return(true, nil)

	deleted, err := pe.Teardown(t.Context(), owner, revision, types.Phase{
		Name: "test",
		Objects: []unstructured.Unstructured{
			*obj, *obj,
		},
	})
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
	owner client.Object,
	revision int64,
	desiredObject Object,
	opts ...types.ObjectReconcileOption,
) (ObjectResult, error) {
	args := m.Called(ctx, owner, revision, desiredObject, opts)

	return args.Get(0).(ObjectResult), args.Error(1)
}

func (m *objectEngineMock) Teardown(
	ctx context.Context,
	owner client.Object,
	revision int64,
	desiredObject Object,
	opts ...types.ObjectTeardownOption,
) (objectDeleted bool, err error) {
	args := m.Called(ctx, owner, revision, desiredObject, opts)

	return args.Bool(0), args.Error(1)
}

type phaseValidatorMock struct {
	mock.Mock
}

func (m *phaseValidatorMock) Validate(
	ctx context.Context,
	owner client.Object,
	phase types.Phase,
) error {
	args := m.Called(ctx, owner, phase)

	return args.Error(0)
}

func TestPhaseResult(t *testing.T) {
	t.Parallel()
	t.Run("InTransistion", func(t *testing.T) {
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
				assert.Equal(t, test.expected, pr.InTransistion())
			})
		}
	})

	t.Run("IsComplete", func(t *testing.T) {
		t.Parallel()

		failedProbeRes := newObjectResultCreated(nil, types.ObjectReconcileOptions{}).(ObjectResultCreated)
		failedProbeRes.probeResults[types.ProgressProbeType] = types.ProbeResult{Status: types.ProbeStatusFalse}

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
