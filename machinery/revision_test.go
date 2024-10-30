package machinery

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/machinery/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestRevisionEngine_Teardown(t *testing.T) {
	t.Parallel()
	pe := &phaseEngineMock{}
	rv := &revisionValidatorMock{}

	re := NewRevisionEngine(pe, rv)

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "12345-678",
			Name:      "owner",
			Namespace: "test",
		},
	}

	rev := Revision{
		Owner:    owner,
		Revision: 3,
		Phases: []Phase{
			{Name: "phase-1"},
			{Name: "phase-2"},
			{Name: "phase-3"},
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

	re := NewRevisionEngine(pe, rv)

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "12345-678",
			Name:      "owner",
			Namespace: "test",
		},
	}

	rev := Revision{
		Owner:    owner,
		Revision: 3,
		Phases: []Phase{
			{Name: "phase-1"},
			{Name: "phase-2"},
			{Name: "phase-3"},
			{Name: "phase-4"},
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
	phase Phase,
) (PhaseResult, error) {
	args := m.Called(ctx, owner, revision, phase)
	return args.Get(0).(PhaseResult), args.Error(1)
}

func (m *phaseEngineMock) Teardown(
	ctx context.Context,
	owner client.Object,
	revision int64,
	phase Phase,
) (PhaseTeardownResult, error) {
	args := m.Called(ctx, owner, revision, phase)
	return args.Get(0).(PhaseTeardownResult), args.Error(1)
}

type revisionValidatorMock struct {
	mock.Mock
}

func (m *revisionValidatorMock) Validate(
	ctx context.Context, rev Revision,
) (validation.RevisionViolation, error) {
	args := m.Called(ctx, rev)
	return args.Get(0).(validation.RevisionViolation), args.Error(1)
}
