package machinery

import (
	"context"
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
		Return(&phaseViolationStub{}, nil)
	oe.On("Reconcile", mock.Anything, owner, revision, obj, mock.Anything).
		Return(newNormalObjectResult(ActionCreated, obj, CompareResult{}, nil), nil)

	ctx := context.Background()
	_, err := pe.Reconcile(ctx, owner, revision, types.Phase{
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
		Return(&phaseViolationStub{
			msg: "xxx",
		}, nil)
	oe.On("Reconcile", mock.Anything, owner, revision, obj, mock.Anything).
		Return(newNormalObjectResult(ActionCreated, obj, CompareResult{}, nil), nil)

	ctx := context.Background()
	_, err := pe.Reconcile(ctx, owner, revision, types.Phase{
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

	ctx := context.Background()
	deleted, err := pe.Teardown(ctx, owner, revision, types.Phase{
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
) (validation.PhaseViolation, error) {
	args := m.Called(ctx, owner, phase)

	return args.Get(0).(validation.PhaseViolation), args.Error(1)
}

type phaseViolationStub struct {
	phaseName string
	objects   []validation.ObjectViolation
	msg       string
}

func (s *phaseViolationStub) PhaseName() string {
	return s.phaseName
}

func (s *phaseViolationStub) Objects() []validation.ObjectViolation {
	return s.objects
}

func (s *phaseViolationStub) Empty() bool {
	return len(s.msg) == 0 && len(s.objects) == 0
}

func (s *phaseViolationStub) Message() string {
	return s.msg
}

func (s *phaseViolationStub) Messages() []string {
	return []string{s.msg}
}

func (s *phaseViolationStub) String() string {
	return s.msg
}

func TestPhaseResult(t *testing.T) {
	t.Parallel()
	t.Run("InTransistion", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name     string
			pv       validation.PhaseViolation
			res      []ObjectResult
			expected bool
		}{
			{
				name: "true - progressed",
				res: []ObjectResult{
					newObjectResultCreated(nil, nil),
					newObjectResultProgressed(nil, CompareResult{}, nil),
				},
				expected: true,
			},
			{
				name: "true - conflict",
				res: []ObjectResult{
					newObjectResultCreated(nil, nil),
					newObjectResultConflict(nil, CompareResult{}, nil, nil),
				},
				expected: true,
			},
			{
				name:     "false - preflight violation",
				pv:       &phaseViolationStub{msg: "xxx"},
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
					newObjectResultCreated(nil, nil),
				},
				expected: false,
			},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				t.Parallel()

				pr := &phaseResult{
					preflightViolation: test.pv,
					objects:            test.res,
				}
				assert.Equal(t, test.expected, pr.InTransistion())
			})
		}
	})

	t.Run("IsComplete", func(t *testing.T) {
		t.Parallel()

		failedProbeRes := newObjectResultCreated(nil, nil).(ObjectResultCreated)
		failedProbeRes.probeResults[types.ProgressProbeType] = ObjectProbeResult{Success: false}

		tests := []struct {
			name     string
			pv       validation.PhaseViolation
			res      []ObjectResult
			expected bool
		}{
			{
				name: "true",
				res: []ObjectResult{
					newObjectResultCreated(nil, nil),
				},
				expected: true,
			},
			{
				name:     "false - preflight violation",
				pv:       &phaseViolationStub{msg: "xxx"},
				res:      []ObjectResult{},
				expected: false,
			},
			{
				name: "false - conflict",
				res: []ObjectResult{
					newObjectResultCreated(nil, nil),
					newObjectResultConflict(nil, CompareResult{}, nil, nil),
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
					preflightViolation: test.pv,
					objects:            test.res,
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
		name:               "phase-1",
		preflightViolation: &phaseViolationStub{msg: "xxx"},
		objects: []ObjectResult{
			newObjectResultCreated(obj, nil),
		},
	}

	assert.Equal(t, `Phase "phase-1"
Complete: false
In Transition: false
Preflight Violation:
  xxx
Objects:
- Object Secret.v1 test/testi
  Action: "Created"
`, r.String())
}
