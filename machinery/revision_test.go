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
	"pkg.package-operator.run/boxcutter/internal/testutil"
	"pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/machinery/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestRevisionEngine_Teardown(t *testing.T) {
	t.Parallel()
	pe := &phaseEngineMock{}
	rv := &revisionValidatorMock{}
	c := testutil.NewClient()
	am := &noopAnchorManager{}

	re := NewRevisionEngine(pe, rv, c, am)

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "12345-678",
			Name:      "owner",
			Namespace: "test",
		},
	}

	rev := &types.RevisionStandin{
		Owner:    owner,
		Revision: 3,
		Phases: []types.Phase{
			&types.PhaseStandin{Name: "phase-1"},
			&types.PhaseStandin{Name: "phase-2"},
			&types.PhaseStandin{Name: "phase-3"},
		},
	}

	pe.
		On("Teardown", mock.Anything, owner, mock.Anything, mock.Anything).
		Return(&phaseTeardownResult{}, nil)

	ctx := context.Background()
	res, err := re.Teardown(ctx, rev)
	require.NoError(t, err)

	assert.True(t, res.IsComplete())
	assert.Equal(t, []string{
		"phase-1", "phase-2", "phase-3",
	}, res.GetGonePhaseNames())
	active, ok := res.GetActivePhaseName()
	assert.False(t, ok)
	assert.Empty(t, active)
	assert.Len(t, res.GetPhases(), 3)
	assert.Empty(t, res.GetWaitingPhaseNames())
}

func TestRevisionEngine_Teardown_delayed(t *testing.T) {
	t.Parallel()
	pe := &phaseEngineMock{}
	rv := &revisionValidatorMock{}
	c := testutil.NewClient()
	am := &noopAnchorManager{}

	re := NewRevisionEngine(pe, rv, c, am)

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "12345-678",
			Name:      "owner",
			Namespace: "test",
		},
	}

	rev := &types.RevisionStandin{
		Owner:    owner,
		Revision: 3,
		Phases: []types.Phase{
			&types.PhaseStandin{Name: "phase-1"},
			&types.PhaseStandin{Name: "phase-2"},
			&types.PhaseStandin{Name: "phase-3"},
			&types.PhaseStandin{Name: "phase-4"},
		},
	}

	pe.
		On("Teardown", mock.Anything, owner, mock.Anything, mock.Anything).
		Twice().
		Return(&phaseTeardownResult{}, nil)
	pe.
		On("Teardown", mock.Anything, owner, mock.Anything, mock.Anything).
		Return(&phaseTeardownResult{waiting: []types.ObjectRef{
			{},
		}}, nil)

	ctx := context.Background()
	res, err := re.Teardown(ctx, rev)
	require.NoError(t, err)

	assert.False(t, res.IsComplete())
	assert.Equal(t, []string{
		"phase-3", "phase-4",
	}, res.GetGonePhaseNames())
	active, ok := res.GetActivePhaseName()
	assert.True(t, ok)
	assert.Equal(t, "phase-2", active)
	assert.Len(t, res.GetPhases(), 3)
	assert.Equal(t, []string{"phase-1"}, res.GetWaitingPhaseNames())
}

type phaseEngineMock struct {
	mock.Mock
}

func (m *phaseEngineMock) Reconcile(
	ctx context.Context,
	owner client.Object,
	revision int64,
	phase types.Phase,
) (PhaseResult, error) {
	args := m.Called(ctx, owner, revision, phase)
	return args.Get(0).(PhaseResult), args.Error(1)
}

func (m *phaseEngineMock) Teardown(
	ctx context.Context,
	owner client.Object,
	revision int64,
	phase types.Phase,
) (PhaseTeardownResult, error) {
	args := m.Called(ctx, owner, revision, phase)
	return args.Get(0).(PhaseTeardownResult), args.Error(1)
}

type revisionValidatorMock struct {
	mock.Mock
}

func (m *revisionValidatorMock) Validate(
	ctx context.Context, rev types.Revision,
) (validation.RevisionViolation, error) {
	args := m.Called(ctx, rev)
	return args.Get(0).(validation.RevisionViolation), args.Error(1)
}

func TestRevisionResult_String(t *testing.T) {
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

	r := revisionResult{
		phases: []string{"phase-1", "phase-2"},
		phasesResults: []PhaseResult{
			&phaseResult{
				name:               "phase-1",
				preflightViolation: &phaseViolationStub{msg: "xxx"},
				objects: []ObjectResult{
					newObjectResultCreated(obj, nil),
				},
			},
		},
	}

	assert.Equal(t, `Revision
Complete: false
In Transition: true
Phases:
- Phase "phase-1"
  Complete: false
  In Transition: false
  Preflight Violation:
    xxx
  Objects:
  - Object Secret.v1 test/testi
    Action: "Created"
- Phase "phase-2" (Pending)
`, r.String())
}

type noopAnchorManager struct{}

// Ensure an anchor for the given object exists and childs have an OwnerReference pointing to the anchor.
func (m *noopAnchorManager) EnsureFor(ctx context.Context, owner client.Object, childs []client.Object) error {
	return nil
}

// Removes the anchor for the given object.
func (m *noopAnchorManager) RemoveFor(ctx context.Context, owner client.Object) error {
	return nil
}
