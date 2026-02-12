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
	"k8s.io/apimachinery/pkg/runtime"

	"pkg.package-operator.run/boxcutter/internal/testutil"
	"pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/ownerhandling"
	"pkg.package-operator.run/boxcutter/validation"
)

func TestRevisionEngine_Teardown(t *testing.T) {
	t.Parallel()

	pe := &phaseEngineMock{}
	rv := &revisionValidatorMock{}
	c := testutil.NewClient()

	re := NewRevisionEngine(pe, rv, c)

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "12345-678",
			Name:      "owner",
			Namespace: "test",
		},
	}

	metadata := ownerhandling.NewNativeRevisionMetadata(owner, scheme)

	rev := types.NewRevision(
		"test",
		metadata,
		3, []types.Phase{
			{Name: "phase-1"},
			{Name: "phase-2"},
			{Name: "phase-3"},
		},
	)

	pe.
		On("Teardown", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&phaseTeardownResult{}, nil)

	res, err := re.Teardown(t.Context(), rev)
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

	re := NewRevisionEngine(pe, rv, c)

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "12345-678",
			Name:      "owner",
			Namespace: "test",
		},
	}

	metadata := ownerhandling.NewNativeRevisionMetadata(owner, scheme)

	rev := types.NewRevision(
		"test",
		metadata,
		3, []types.Phase{
			{Name: "phase-1"},
			{Name: "phase-2"},
			{Name: "phase-3"},
			{Name: "phase-4"},
		},
	)

	pe.
		On("Teardown", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Twice().
		Return(&phaseTeardownResult{}, nil)
	pe.
		On("Teardown", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&phaseTeardownResult{waiting: []types.ObjectRef{
			{},
		}}, nil)

	res, err := re.Teardown(t.Context(), rev)
	require.NoError(t, err)

	assert.False(t, res.IsComplete())
	assert.Equal(t, []string{"phase-3", "phase-4"}, res.GetGonePhaseNames())

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
	metadata types.RevisionMetadata,
	revision int64,
	phase types.Phase,
	opts ...types.PhaseReconcileOption,
) (PhaseResult, error) {
	args := m.Called(ctx, metadata, revision, phase, opts)

	return args.Get(0).(PhaseResult), args.Error(1)
}

func (m *phaseEngineMock) Teardown(
	ctx context.Context,
	metadata types.RevisionMetadata,
	revision int64,
	phase types.Phase,
	opts ...types.PhaseTeardownOption,
) (PhaseTeardownResult, error) {
	args := m.Called(ctx, metadata, revision, phase, opts)

	return args.Get(0).(PhaseTeardownResult), args.Error(1)
}

type revisionValidatorMock struct {
	mock.Mock
}

func (m *revisionValidatorMock) Validate(
	ctx context.Context, rev types.Revision,
) error {
	args := m.Called(ctx, rev)

	return args.Error(0)
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
				name: "phase-1",
				validationError: &validation.PhaseValidationError{
					PhaseError: errTest,
				},
				objects: []ObjectResult{
					newObjectResultCreated(obj, types.ObjectReconcileOptions{}),
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
  Validation Errors:
  - AAAAAAh
  Objects:
  - Object Secret.v1 test/testi
    Action: "Created"
- Phase "phase-2" (Pending)
`, r.String())
}
