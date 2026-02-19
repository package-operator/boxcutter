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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"pkg.package-operator.run/boxcutter/internal/testutil"
	"pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/validation"
)

func TestRevisionEngine_Teardown(t *testing.T) {
	t.Parallel()

	pe := &phaseEngineMock{}
	rv := &revisionValidatorMock{}
	c := testutil.NewClient()

	re := NewRevisionEngine(pe, rv, c)

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "12345-678",
			Name:      "owner",
			Namespace: "test",
		},
	}

	rev := types.NewRevision(
		"test", 3,
		[]types.Phase{
			types.NewPhase("phase-1", nil),
			types.NewPhase("phase-2", nil),
			types.NewPhase("phase-3", nil),
		},
	)

	pe.
		On("Teardown", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&phaseTeardownResult{}, nil)

	res, err := re.Teardown(t.Context(), rev, types.WithOwner(owner, nil))
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

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "12345-678",
			Name:      "owner",
			Namespace: "test",
		},
	}

	rev := types.NewRevision(
		"test", 3,
		[]types.Phase{
			types.NewPhase("phase-1", nil),
			types.NewPhase("phase-2", nil),
			types.NewPhase("phase-3", nil),
			types.NewPhase("phase-4", nil),
		},
	)

	pe.
		On("Teardown", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Twice().
		Return(&phaseTeardownResult{}, nil)
	pe.
		On("Teardown", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&phaseTeardownResult{waiting: []types.ObjectRef{
			{},
		}}, nil)

	res, err := re.Teardown(t.Context(), rev, types.WithOwner(owner, nil))
	require.NoError(t, err)

	assert.False(t, res.IsComplete())
	assert.Equal(t, []string{"phase-3", "phase-4"}, res.GetGonePhaseNames())

	active, ok := res.GetActivePhaseName()
	assert.True(t, ok)
	assert.Equal(t, "phase-2", active)
	assert.Len(t, res.GetPhases(), 3)
	assert.Equal(t, []string{"phase-1"}, res.GetWaitingPhaseNames())
}

func TestRevisionEngine_Reconcile_Success_AllPhasesComplete(t *testing.T) {
	t.Parallel()

	pe := &phaseEngineMock{}
	rv := &revisionValidatorMock{}
	c := testutil.NewClient()

	re := NewRevisionEngine(pe, rv, c)

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "12345",
			Name:      "owner",
			Namespace: "test",
		},
	}

	obj1 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "obj1",
				"namespace": "test",
			},
		},
	}

	obj2 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "obj2",
				"namespace": "test",
			},
		},
	}

	obj3 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Service",
			"metadata": map[string]interface{}{
				"name":      "obj3",
				"namespace": "test",
			},
		},
	}

	// Create revision with 3 phases
	rev := types.NewRevision("test", 3, []types.Phase{
		types.NewPhase("phase-1", []client.Object{obj1}),
		types.NewPhase("phase-2", []client.Object{obj2}),
		types.NewPhase("phase-3", []client.Object{obj3}),
	})

	// Mock: validation passes
	rv.On("Validate", mock.Anything, rev).Return(nil)

	// Mock: all phases complete successfully
	pe.On("Reconcile", mock.Anything, int64(3), mock.Anything, mock.Anything).
		Return(&testPhaseResult{complete: true, progressed: true}, nil).
		Times(3)

	// Execute
	res, err := re.Reconcile(context.Background(), rev, types.WithOwner(owner, nil))

	// Assert
	require.NoError(t, err)
	assert.True(t, res.IsComplete())
	assert.False(t, res.InTransition())
	assert.Len(t, res.GetPhases(), 3)
	assert.Nil(t, res.GetValidationError())
	pe.AssertExpectations(t)
	rv.AssertExpectations(t)
}

func TestRevisionEngine_Reconcile_WaitsOnIncompletePhase(t *testing.T) {
	t.Parallel()

	pe := &phaseEngineMock{}
	rv := &revisionValidatorMock{}
	c := testutil.NewClient()

	re := NewRevisionEngine(pe, rv, c)

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "12345",
			Name:      "owner",
			Namespace: "test",
		},
	}

	obj1 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "obj1",
				"namespace": "test",
			},
		},
	}

	rev := types.NewRevision("test", 3, []types.Phase{
		types.NewPhase("phase-1", []client.Object{obj1}),
		types.NewPhase("phase-2", nil),
		types.NewPhase("phase-3", nil),
	})

	// Mock: validation passes
	rv.On("Validate", mock.Anything, rev).Return(nil)

	// First phase incomplete, second should never be called
	pe.On("Reconcile", mock.Anything, int64(3), mock.Anything, mock.Anything).
		Return(&testPhaseResult{complete: false}, nil).
		Once()

	res, err := re.Reconcile(context.Background(), rev, types.WithOwner(owner, nil))

	require.NoError(t, err)
	assert.False(t, res.IsComplete())
	assert.True(t, res.InTransition())
	assert.Len(t, res.GetPhases(), 1) // Only first phase

	// Verify second phase was never reconciled
	pe.AssertNumberOfCalls(t, "Reconcile", 1)
	rv.AssertExpectations(t)
}

func TestRevisionEngine_Reconcile_ValidationError(t *testing.T) {
	t.Parallel()

	pe := &phaseEngineMock{}
	rv := &revisionValidatorMock{}
	c := testutil.NewClient()

	re := NewRevisionEngine(pe, rv, c)

	rev := types.NewRevision("test", 3, []types.Phase{
		types.NewPhase("phase-1", nil),
	})

	// Mock: validation returns RevisionValidationError
	validationErr := &validation.RevisionValidationError{
		RevisionName:   "test",
		RevisionNumber: 3,
		Phases: []validation.PhaseValidationError{
			{
				PhaseError: errTest,
			},
		},
	}
	rv.On("Validate", mock.Anything, rev).Return(validationErr)

	res, err := re.Reconcile(context.Background(), rev)

	// Should NOT error out, but capture validation error in result
	require.NoError(t, err)
	assert.NotNil(t, res.GetValidationError())
	assert.Equal(t, validationErr, res.GetValidationError())
	assert.False(t, res.InTransition()) // Validation errors block transition
	assert.Empty(t, res.GetPhases())    // No phases reconciled

	// Verify no phase reconciliation attempted
	pe.AssertNotCalled(t, "Reconcile")
	rv.AssertExpectations(t)
}

func TestRevisionEngine_Reconcile_ValidationFailure(t *testing.T) {
	t.Parallel()

	pe := &phaseEngineMock{}
	rv := &revisionValidatorMock{}
	c := testutil.NewClient()

	re := NewRevisionEngine(pe, rv, c)

	rev := types.NewRevision("test", 3, []types.Phase{
		types.NewPhase("phase-1", nil),
	})

	// Mock: validation returns non-RevisionValidationError
	rv.On("Validate", mock.Anything, rev).
		Return(errTest)

	res, err := re.Reconcile(context.Background(), rev)

	// Should error out
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validating")

	// Result still returned but phases not reconciled
	assert.Empty(t, res.GetPhases())

	// Verify no phase reconciliation attempted
	pe.AssertNotCalled(t, "Reconcile")
	rv.AssertExpectations(t)
}

func TestRevisionEngine_Reconcile_PhaseReconcileError(t *testing.T) {
	t.Parallel()

	pe := &phaseEngineMock{}
	rv := &revisionValidatorMock{}
	c := testutil.NewClient()

	re := NewRevisionEngine(pe, rv, c)

	rev := types.NewRevision("test", 3, []types.Phase{
		types.NewPhase("phase-1", nil),
		types.NewPhase("phase-2", nil),
		types.NewPhase("phase-3", nil),
	})

	// Mock: validation passes
	rv.On("Validate", mock.Anything, rev).Return(nil)

	// First phase succeeds
	pe.On("Reconcile", mock.Anything, int64(3), mock.Anything, mock.Anything).
		Return(&testPhaseResult{complete: true}, nil).
		Once()

	// Second phase errors
	pe.On("Reconcile", mock.Anything, int64(3), mock.Anything, mock.Anything).
		Return(&testPhaseResult{}, errTest).
		Once()

	res, err := re.Reconcile(context.Background(), rev)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reconciling object")
	assert.Len(t, res.GetPhases(), 1) // Only first phase in result

	pe.AssertExpectations(t)
	rv.AssertExpectations(t)
}

func TestRevisionEngine_Reconcile_WithRevisionOptions(t *testing.T) {
	t.Parallel()

	pe := &phaseEngineMock{}
	rv := &revisionValidatorMock{}
	c := testutil.NewClient()

	re := NewRevisionEngine(pe, rv, c)

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "12345",
			Name:      "owner",
			Namespace: "test",
		},
	}

	testObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "test-obj",
				"namespace": "test",
			},
		},
	}

	// Create a simple prober for testing
	testProber := types.ProbeFunc(func(client.Object) types.ProbeResult {
		return types.ProbeResult{Status: types.ProbeStatusTrue}
	})

	// Create revision with phase-level options
	rev := types.NewRevision("test", 1, []types.Phase{
		types.NewPhase("phase-1", []client.Object{testObj}).
			WithReconcileOptions(types.WithProbe("test", testProber)),
	})

	// Mock: validation passes
	rv.On("Validate", mock.Anything, rev).Return(nil)

	// Verify options are merged and forwarded
	pe.On("Reconcile", mock.Anything, int64(1), mock.Anything,
		mock.MatchedBy(func(opts []types.PhaseReconcileOption) bool {
			// Verify options were passed (includes phase options + owner)
			return len(opts) > 0
		})).
		Return(&testPhaseResult{complete: true}, nil)

	res, err := re.Reconcile(context.Background(), rev, types.WithOwner(owner, nil))

	require.NoError(t, err)
	assert.True(t, res.IsComplete())
	pe.AssertExpectations(t)
	rv.AssertExpectations(t)
}

func TestRevisionResult_HasProgressed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		phases   []string
		results  []PhaseResult
		expected bool
	}{
		{
			name:   "all phases progressed",
			phases: []string{"p1", "p2", "p3"},
			results: []PhaseResult{
				&testPhaseResult{progressed: true},
				&testPhaseResult{progressed: true},
				&testPhaseResult{progressed: true},
			},
			expected: true,
		},
		{
			name:   "some phases progressed",
			phases: []string{"p1", "p2", "p3"},
			results: []PhaseResult{
				&testPhaseResult{progressed: true},
				&testPhaseResult{progressed: false},
				&testPhaseResult{progressed: true},
			},
			expected: false,
		},
		{
			name:   "no phases progressed",
			phases: []string{"p1", "p2"},
			results: []PhaseResult{
				&testPhaseResult{progressed: false},
				&testPhaseResult{progressed: false},
			},
			expected: false,
		},
		{
			name:     "empty phases",
			phases:   []string{},
			results:  []PhaseResult{},
			expected: true, // 0 == 0 is true
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &revisionResult{
				phases:        tt.phases,
				phasesResults: tt.results,
			}
			assert.Equal(t, tt.expected, r.HasProgressed())
		})
	}
}

func TestRevisionResult_IsComplete_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		phases   []string
		results  []PhaseResult
		expected bool
	}{
		{
			name:   "not all phases acted on",
			phases: []string{"p1", "p2", "p3"},
			results: []PhaseResult{
				&testPhaseResult{complete: true},
				&testPhaseResult{complete: true},
				// Missing p3
			},
			expected: false, // Line 124-127
		},
		{
			name:   "all phases acted but some incomplete",
			phases: []string{"p1", "p2"},
			results: []PhaseResult{
				&testPhaseResult{complete: true},
				&testPhaseResult{complete: false},
			},
			expected: false, // Line 129-132
		},
		{
			name:   "all phases complete",
			phases: []string{"p1", "p2"},
			results: []PhaseResult{
				&testPhaseResult{complete: true},
				&testPhaseResult{complete: true},
			},
			expected: true, // Line 135
		},
		{
			name:     "empty phases list",
			phases:   []string{},
			results:  []PhaseResult{},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &revisionResult{
				phases:        tt.phases,
				phasesResults: tt.results,
			}
			assert.Equal(t, tt.expected, r.IsComplete())
		})
	}
}

func TestRevisionResult_InTransition_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		phases          []string
		results         []PhaseResult
		validationError *validation.RevisionValidationError
		expected        bool
	}{
		{
			name:   "phase in transition",
			phases: []string{"p1", "p2"},
			results: []PhaseResult{
				&testPhaseResult{inTransition: true},
				&testPhaseResult{inTransition: false},
			},
			expected: true, // Line 90-93
		},
		{
			name:    "validation error present",
			phases:  []string{"p1"},
			results: []PhaseResult{},
			validationError: &validation.RevisionValidationError{
				RevisionName:   "test",
				RevisionNumber: 1,
			},
			expected: false, // Line 96-98
		},
		{
			name:   "not all phases acted on",
			phases: []string{"p1", "p2", "p3"},
			results: []PhaseResult{
				&testPhaseResult{inTransition: false},
			},
			expected: true, // Line 100-103
		},
		{
			name:   "all complete no transition",
			phases: []string{"p1", "p2"},
			results: []PhaseResult{
				&testPhaseResult{inTransition: false},
				&testPhaseResult{inTransition: false},
			},
			expected: false, // Line 105
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &revisionResult{
				phases:          tt.phases,
				phasesResults:   tt.results,
				validationError: tt.validationError,
			}
			assert.Equal(t, tt.expected, r.InTransition())
		})
	}
}

func TestRevisionResult_GetPhases(t *testing.T) {
	t.Parallel()

	t.Run("returns phase results", func(t *testing.T) {
		t.Parallel()

		results := []PhaseResult{
			&testPhaseResult{name: "p1"},
			&testPhaseResult{name: "p2"},
		}
		r := &revisionResult{phasesResults: results}

		assert.Equal(t, results, r.GetPhases())
	})

	t.Run("empty list", func(t *testing.T) {
		t.Parallel()

		r := &revisionResult{}
		assert.Empty(t, r.GetPhases())
	})
}

// testPhaseResult is a simple test implementation of PhaseResult.
type testPhaseResult struct {
	complete      bool
	progressed    bool
	inTransition  bool
	name          string
	validationErr *validation.PhaseValidationError
	objects       []ObjectResult
}

func (r *testPhaseResult) GetName() string {
	return r.name
}

func (r *testPhaseResult) GetValidationError() *validation.PhaseValidationError {
	return r.validationErr
}

func (r *testPhaseResult) GetObjects() []ObjectResult {
	return r.objects
}

func (r *testPhaseResult) InTransition() bool {
	return r.inTransition
}

func (r *testPhaseResult) IsComplete() bool {
	return r.complete
}

func (r *testPhaseResult) HasProgressed() bool {
	return r.progressed
}

func (r *testPhaseResult) String() string {
	return ""
}

type phaseEngineMock struct {
	mock.Mock
}

func (m *phaseEngineMock) Reconcile(
	ctx context.Context,
	revision int64,
	phase types.Phase,
	opts ...types.PhaseReconcileOption,
) (PhaseResult, error) {
	args := m.Called(ctx, revision, phase, opts)

	return args.Get(0).(PhaseResult), args.Error(1)
}

func (m *phaseEngineMock) Teardown(
	ctx context.Context,
	revision int64,
	phase types.Phase,
	opts ...types.PhaseTeardownOption,
) (PhaseTeardownResult, error) {
	args := m.Called(ctx, revision, phase, opts)

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

func TestRevisionTeardownResult_String_Complete(t *testing.T) {
	t.Parallel()

	r := revisionTeardownResult{
		phases: []PhaseTeardownResult{
			&phaseTeardownResult{
				name: "phase-1",
				gone: []types.ObjectRef{
					{
						GroupVersionKind: schema.GroupVersionKind{
							Group:   "",
							Version: "v1",
							Kind:    "ConfigMap",
						},
						ObjectKey: client.ObjectKey{
							Name:      "test-cm",
							Namespace: "default",
						},
					},
				},
			},
			&phaseTeardownResult{
				name: "phase-2",
				gone: []types.ObjectRef{
					{
						GroupVersionKind: schema.GroupVersionKind{
							Group:   "",
							Version: "v1",
							Kind:    "Secret",
						},
						ObjectKey: client.ObjectKey{
							Name:      "test-secret",
							Namespace: "default",
						},
					},
				},
			},
		},
		active: "",
		gone:   []string{"phase-1", "phase-2"},
	}

	expected := `Revision Teardown
Gone Phases:
- phase-1
- phase-2
Phases:
- Phase "phase-1"
  Gone Objects:
  - /v1, Kind=ConfigMap default/test-cm
- Phase "phase-2"
  Gone Objects:
  - /v1, Kind=Secret default/test-secret
`

	assert.Equal(t, expected, r.String())
}

func TestRevisionTeardownResult_String_WithActiveAndWaiting(t *testing.T) {
	t.Parallel()

	r := revisionTeardownResult{
		phases: []PhaseTeardownResult{
			&phaseTeardownResult{
				name: "phase-3",
				gone: []types.ObjectRef{
					{
						GroupVersionKind: schema.GroupVersionKind{
							Group:   "",
							Version: "v1",
							Kind:    "Service",
						},
						ObjectKey: client.ObjectKey{
							Name:      "my-service",
							Namespace: "test-ns",
						},
					},
				},
			},
			&phaseTeardownResult{
				name: "phase-2",
				waiting: []types.ObjectRef{
					{
						GroupVersionKind: schema.GroupVersionKind{
							Group:   "apps",
							Version: "v1",
							Kind:    "Deployment",
						},
						ObjectKey: client.ObjectKey{
							Name:      "my-deployment",
							Namespace: "test-ns",
						},
					},
				},
			},
		},
		active:  "phase-2",
		waiting: []string{"phase-1"},
		gone:    []string{"phase-3"},
	}

	expected := `Revision Teardown
Active: phase-2
Waiting Phases:
- phase-1
Gone Phases:
- phase-3
Phases:
- Phase "phase-3"
  Gone Objects:
  - /v1, Kind=Service test-ns/my-service
- Phase "phase-2"
  Waiting Objects:
  - apps/v1, Kind=Deployment test-ns/my-deployment
`

	assert.Equal(t, expected, r.String())
}

func TestRevisionTeardownResult_String_Empty(t *testing.T) {
	t.Parallel()

	r := revisionTeardownResult{
		phases: []PhaseTeardownResult{},
	}

	expected := `Revision Teardown
Phases:
`

	assert.Equal(t, expected, r.String())
}

func TestRevisionTeardownResult_String_OnlyActive(t *testing.T) {
	t.Parallel()

	r := revisionTeardownResult{
		phases: []PhaseTeardownResult{
			&phaseTeardownResult{
				name: "phase-1",
				waiting: []types.ObjectRef{
					{
						GroupVersionKind: schema.GroupVersionKind{
							Group:   "",
							Version: "v1",
							Kind:    "Pod",
						},
						ObjectKey: client.ObjectKey{
							Name:      "my-pod",
							Namespace: "kube-system",
						},
					},
				},
			},
		},
		active: "phase-1",
	}

	expected := `Revision Teardown
Active: phase-1
Phases:
- Phase "phase-1"
  Waiting Objects:
  - /v1, Kind=Pod kube-system/my-pod
`

	assert.Equal(t, expected, r.String())
}

func TestRevisionTeardownResult_String_MultipleWaitingAndGone(t *testing.T) {
	t.Parallel()

	r := revisionTeardownResult{
		phases: []PhaseTeardownResult{
			&phaseTeardownResult{
				name: "phase-4",
				gone: []types.ObjectRef{
					{
						GroupVersionKind: schema.GroupVersionKind{
							Group:   "rbac.authorization.k8s.io",
							Version: "v1",
							Kind:    "Role",
						},
						ObjectKey: client.ObjectKey{
							Name:      "admin-role",
							Namespace: "default",
						},
					},
				},
			},
			&phaseTeardownResult{
				name: "phase-3",
				gone: []types.ObjectRef{
					{
						GroupVersionKind: schema.GroupVersionKind{
							Group:   "",
							Version: "v1",
							Kind:    "ServiceAccount",
						},
						ObjectKey: client.ObjectKey{
							Name:      "my-sa",
							Namespace: "default",
						},
					},
				},
			},
		},
		active:  "phase-2",
		waiting: []string{"phase-1"},
		gone:    []string{"phase-3", "phase-4"},
	}

	expected := `Revision Teardown
Active: phase-2
Waiting Phases:
- phase-1
Gone Phases:
- phase-3
- phase-4
Phases:
- Phase "phase-4"
  Gone Objects:
  - rbac.authorization.k8s.io/v1, Kind=Role default/admin-role
- Phase "phase-3"
  Gone Objects:
  - /v1, Kind=ServiceAccount default/my-sa
`

	assert.Equal(t, expected, r.String())
}
