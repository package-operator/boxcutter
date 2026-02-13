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

	rev := types.Revision{
		Owner:    owner,
		Revision: 3,
		Phases: []types.Phase{
			{Name: "phase-1"},
			{Name: "phase-2"},
			{Name: "phase-3"},
		},
	}

	pe.
		On("Teardown", mock.Anything, owner, mock.Anything, mock.Anything, mock.Anything).
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

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "12345-678",
			Name:      "owner",
			Namespace: "test",
		},
	}

	rev := types.Revision{
		Owner:    owner,
		Revision: 3,
		Phases: []types.Phase{
			{Name: "phase-1"},
			{Name: "phase-2"},
			{Name: "phase-3"},
			{Name: "phase-4"},
		},
	}

	pe.
		On("Teardown", mock.Anything, owner, mock.Anything, mock.Anything, mock.Anything).
		Twice().
		Return(&phaseTeardownResult{}, nil)
	pe.
		On("Teardown", mock.Anything, owner, mock.Anything, mock.Anything, mock.Anything).
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
	owner client.Object,
	revision int64,
	phase types.Phase,
	opts ...types.PhaseReconcileOption,
) (PhaseResult, error) {
	args := m.Called(ctx, owner, revision, phase, opts)

	return args.Get(0).(PhaseResult), args.Error(1)
}

func (m *phaseEngineMock) Teardown(
	ctx context.Context,
	owner client.Object,
	revision int64,
	phase types.Phase,
	opts ...types.PhaseTeardownOption,
) (PhaseTeardownResult, error) {
	args := m.Called(ctx, owner, revision, phase, opts)

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
