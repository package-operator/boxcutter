package validation

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"pkg.package-operator.run/boxcutter/machinery/types"
)

var (
	errTest    = errors.New("AAAAAAh")
	errTest2   = errors.New("different AAh")
	testObjRef = types.ObjectRef{
		GroupVersionKind: schema.GroupVersionKind{
			Group:   "banana",
			Version: "v1",
			Kind:    "Cavendish",
		},
		ObjectKey: client.ObjectKey{
			Name:      "b-1",
			Namespace: "bananas",
		},
	}
)

func TestNewObjectValidationError(t *testing.T) {
	t.Parallel()

	e := NewObjectValidationError(types.ObjectRef{})
	assert.NoError(t, e)
}

func TestObjectValidationError_Unwrap(t *testing.T) {
	t.Parallel()

	e := NewObjectValidationError(types.ObjectRef{}, errTest)
	require.ErrorIs(t, e, errTest)
}

func TestObjectValidationError_String(t *testing.T) {
	t.Parallel()

	e := ObjectValidationError{ObjectRef: testObjRef, Errors: []error{errTest}}
	assert.Equal(t, `banana/v1, Kind=Cavendish bananas/b-1:
- AAAAAAh
`, e.String())
}

func TestObjectValidationError_Error(t *testing.T) {
	t.Parallel()

	e := NewObjectValidationError(testObjRef, errTest, errTest)
	assert.Equal(t, `banana/v1, Kind=Cavendish bananas/b-1: AAAAAAh, AAAAAAh`, e.Error())
}

func TestNewPhaseValidationError(t *testing.T) {
	t.Parallel()

	e := NewPhaseValidationError("", nil)
	assert.NoError(t, e)
}

func TestPhaseValidationError_Error_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		err       PhaseValidationError
		errOutput string
		strOutput string
	}{
		{
			name: "with phaseErr and object errors",
			err: PhaseValidationError{
				PhaseName:  "testPhase",
				PhaseError: errTest,
				Objects: []ObjectValidationError{
					{
						ObjectRef: testObjRef, Errors: []error{errTest},
					},
				},
			},
			errOutput: `phase "testPhase": AAAAAAh, banana/v1, Kind=Cavendish bananas/b-1: AAAAAAh`,
			strOutput: `testPhase:
- AAAAAAh
- banana/v1, Kind=Cavendish bananas/b-1:
  - AAAAAAh
`,
		},
		{
			name: "with only phaseErr",
			err: PhaseValidationError{
				PhaseName:  "testPhase",
				PhaseError: errTest,
			},
			errOutput: `phase "testPhase": AAAAAAh`,
			strOutput: `testPhase:
- AAAAAAh
`,
		},
		{
			name: "with only object errs",
			err: PhaseValidationError{
				PhaseName: "testPhase",
				Objects: []ObjectValidationError{
					{
						ObjectRef: testObjRef, Errors: []error{errTest},
					},
				},
			},
			errOutput: `phase "testPhase": banana/v1, Kind=Cavendish bananas/b-1: AAAAAAh`,
			strOutput: `testPhase:
- banana/v1, Kind=Cavendish bananas/b-1:
  - AAAAAAh
`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.errOutput, test.err.Error())
			assert.Equal(t, test.strOutput, test.err.String())
		})
	}
}

func TestNewRevisionValidationError(t *testing.T) {
	t.Parallel()

	e := NewRevisionValidationError("", 0)
	assert.NoError(t, e)
}

//nolint:errorlint
func TestPhaseValidationError_Unwrap(t *testing.T) {
	t.Parallel()

	e := NewPhaseValidationError(
		"testPhase",
		errTest,
		*NewObjectValidationError(testObjRef, errTest2).(*ObjectValidationError),
	)
	require.ErrorIs(t, e, errTest)

	var objErr ObjectValidationError

	require.ErrorAs(t, e, &objErr)
	require.ErrorIs(t, e, errTest2)
}

var errRevisionValidation = RevisionValidationError{
	RevisionName:   "testRev",
	RevisionNumber: 123,
	Phases: []PhaseValidationError{
		{
			PhaseName:  "testPhase",
			PhaseError: errTest,
			Objects: []ObjectValidationError{
				{
					ObjectRef: testObjRef, Errors: []error{errTest2},
				},
			},
		},
	},
}

func TestRevisionValidationError_String(t *testing.T) {
	t.Parallel()

	e := errRevisionValidation
	assert.Equal(t, `testRev (123):
- testPhase:
  - AAAAAAh
  - banana/v1, Kind=Cavendish bananas/b-1:
    - different AAh
`, e.String())
}

func TestRevisionValidationError_Error(t *testing.T) {
	t.Parallel()

	e := errRevisionValidation
	assert.Equal(t, `revision "testRev" (123): phase "testPhase": `+
		`AAAAAAh, banana/v1, Kind=Cavendish bananas/b-1: different AAh`, e.Error())
}

func TestRevisionValidationError_Unwrap(t *testing.T) {
	t.Parallel()

	e := errRevisionValidation
	require.ErrorIs(t, e, errTest)

	var objErr ObjectValidationError

	require.ErrorAs(t, e, &objErr)
	require.ErrorIs(t, e, errTest2)
}
