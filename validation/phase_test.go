package validation

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/ownerhandling"
)

type mockObjectValidator struct {
	mock.Mock
}

func (m *mockObjectValidator) Validate(ctx context.Context, metadata types.RevisionMetadata, obj *unstructured.Unstructured) error {
	args := m.Called(ctx, metadata, obj)

	return args.Error(0)
}

type testablePhaseValidator struct {
	validator        *PhaseValidator
	mockObjValidator *mockObjectValidator
}

func (v *testablePhaseValidator) Validate(ctx context.Context, metadata types.RevisionMetadata, phase types.Phase) error {
	phaseError := validatePhaseName(phase)

	var (
		objectErrors []ObjectValidationError
		errs         []error
	)

	for _, o := range phase.GetObjects() {
		obj := &o

		err := v.mockObjValidator.Validate(ctx, metadata, obj)
		if err == nil {
			continue
		}

		var oerr *ObjectValidationError
		if errors.As(err, &oerr) {
			objectErrors = append(objectErrors, *oerr)
		} else {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	objectErrors = append(objectErrors, checkForObjectDuplicates(phase)...)

	result := NewPhaseValidationError(
		phase.GetName(), phaseError, compactObjectViolations(objectErrors)...)

	return result
}

func TestNewPhaseValidator(t *testing.T) {
	t.Parallel()

	restMapper := &mockRestMapper{}
	writer := &mockWriter{}

	validator := NewPhaseValidator(restMapper, writer)

	assert.NotNil(t, validator)
	assert.NotNil(t, validator.ObjectValidator)
}

func TestPhaseValidator_Validate(t *testing.T) {
	t.Parallel()

	scheme := testScheme()

	tests := []struct {
		name                     string
		phase                    types.Phase
		ownerNamespace           string
		mockSetup                func(*mockObjectValidator)
		expectError              bool
		expectPhaseValidationErr bool
		useRealValidator         bool
	}{
		{
			name: "valid phase",
			phase: types.Phase{
				Name: "valid-phase",
				Objects: []unstructured.Unstructured{
					{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ConfigMap",
							"metadata": map[string]interface{}{
								"name":      "test1",
								"namespace": "default",
							},
						},
					},
				},
			},
			ownerNamespace: "default",
			mockSetup: func(objValidator *mockObjectValidator) {
				objValidator.On("Validate", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectError: false,
		},
		{
			name: "invalid phase name",
			phase: types.Phase{
				Name: "Invalid_Phase_Name",
				Objects: []unstructured.Unstructured{
					{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ConfigMap",
							"metadata": map[string]interface{}{
								"name":      "test1",
								"namespace": "default",
							},
						},
					},
				},
			},
			ownerNamespace: "default",
			mockSetup: func(objValidator *mockObjectValidator) {
				objValidator.On("Validate", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectError:              true,
			expectPhaseValidationErr: true,
		},
		{
			name: "object validation error from mock",
			phase: types.Phase{
				Name: "valid-phase",
				Objects: []unstructured.Unstructured{
					{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ConfigMap",
							"metadata": map[string]interface{}{
								"name":      "test1",
								"namespace": "default",
							},
						},
					},
				},
			},
			ownerNamespace: "default",
			mockSetup: func(objValidator *mockObjectValidator) {
				objErr := &ObjectValidationError{
					ObjectRef: testObjRef,
					Errors:    []error{errTest},
				}
				objValidator.On("Validate", mock.Anything, mock.Anything, mock.Anything).Return(objErr)
			},
			expectError:              true,
			expectPhaseValidationErr: true,
			useRealValidator:         false,
		},
		{
			name: "unknown error during object validation",
			phase: types.Phase{
				Name: "valid-phase",
				Objects: []unstructured.Unstructured{
					{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ConfigMap",
							"metadata": map[string]interface{}{
								"name":      "test1",
								"namespace": "default",
							},
						},
					},
				},
			},
			ownerNamespace: "default",
			mockSetup: func(objValidator *mockObjectValidator) {
				objValidator.On("Validate", mock.Anything, mock.Anything, mock.Anything).Return(
					errors.New("unknown error"))
			},
			expectError:              true,
			expectPhaseValidationErr: false,
		},
		{
			name: "duplicate objects in same phase",
			phase: types.Phase{
				Name: "valid-phase",
				Objects: []unstructured.Unstructured{
					{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ConfigMap",
							"metadata": map[string]interface{}{
								"name":      "test1",
								"namespace": "default",
							},
						},
					},
					{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ConfigMap",
							"metadata": map[string]interface{}{
								"name":      "test1",
								"namespace": "default",
							},
						},
					},
				},
			},
			ownerNamespace: "default",
			mockSetup: func(objValidator *mockObjectValidator) {
				objValidator.On("Validate", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectError:              true,
			expectPhaseValidationErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var err error

			var objValidator *mockObjectValidator

			owner := testOwner(test.ownerNamespace)
			metadata := ownerhandling.NewNativeRevisionMetadata(owner, scheme)

			if test.useRealValidator {
				restMapper := &mockRestMapper{}
				writer := &mockWriter{}

				restMapper.On("RESTMapping", mock.Anything, mock.Anything).Return(
					&meta.RESTMapping{Scope: meta.RESTScopeNamespace}, nil)
				writer.On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

				realValidator := NewPhaseValidator(restMapper, writer)
				err = realValidator.Validate(t.Context(), metadata, test.phase)
			} else {
				objValidator = &mockObjectValidator{}
				test.mockSetup(objValidator)

				validator := &testablePhaseValidator{
					validator:        &PhaseValidator{ObjectValidator: &ObjectValidator{}},
					mockObjValidator: objValidator,
				}

				err = validator.Validate(t.Context(), metadata, test.phase)
			}

			if test.expectError {
				require.Error(t, err)

				if test.expectPhaseValidationErr {
					var phaseErr *PhaseValidationError

					require.ErrorAs(t, err, &phaseErr)
				}
			} else {
				require.NoError(t, err)
			}

			if !test.useRealValidator && objValidator != nil {
				objValidator.AssertExpectations(t)
			}
		})
	}
}

func TestPhaseNameInvalidError(t *testing.T) {
	t.Parallel()

	err := PhaseNameInvalidError{
		PhaseName:     "Invalid_Name",
		ErrorMessages: []string{"contains invalid characters", "too long"},
	}

	assert.Equal(t, "phase name invalid: contains invalid characters, too long", err.Error())
}

func TestValidatePhaseName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		phase       types.Phase
		expectError bool
	}{
		{
			name: "valid phase name",
			phase: types.Phase{
				Name: "valid-phase-name",
			},
			expectError: false,
		},
		{
			name: "invalid phase name with uppercase",
			phase: types.Phase{
				Name: "Invalid-Phase-Name",
			},
			expectError: true,
		},
		{
			name: "invalid phase name with underscores",
			phase: types.Phase{
				Name: "invalid_phase_name",
			},
			expectError: true,
		},
		{
			name: "invalid phase name too long",
			phase: types.Phase{
				Name: "this-is-a-very-long-phase-name-that-exceeds-the-dns1035-label-length-limit-of-63-characters",
			},
			expectError: true,
		},
		{
			name: "empty phase name",
			phase: types.Phase{
				Name: "",
			},
			expectError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := validatePhaseName(test.phase)

			if test.expectError {
				require.Error(t, err)

				var phaseNameErr PhaseNameInvalidError

				assert.ErrorAs(t, err, &phaseNameErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPhaseNameValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		phaseName string
		expectErr bool
	}{
		{
			name:      "valid name",
			phaseName: "valid-phase",
			expectErr: false,
		},
		{
			name:      "invalid name with uppercase",
			phaseName: "Invalid",
			expectErr: true,
		},
		{
			name:      "invalid name with underscores",
			phaseName: "invalid_name",
			expectErr: true,
		},
		{
			name:      "empty name",
			phaseName: "",
			expectErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			errs := phaseNameValid(test.phaseName)

			if test.expectErr {
				assert.NotEmpty(t, errs)
			} else {
				assert.Empty(t, errs)
			}
		})
	}
}

func TestPhaseObjectDuplicationError(t *testing.T) {
	t.Parallel()

	err := PhaseObjectDuplicationError{
		PhaseNames: []string{"phase1", "phase2", "phase3"},
	}

	assert.Equal(t, "duplicate object found in phases: phase1, phase2, phase3", err.Error())
}

func TestCheckForObjectDuplicates(t *testing.T) {
	t.Parallel()

	obj1 := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "test1",
				"namespace": "default",
			},
		},
	}

	obj2 := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "test2",
				"namespace": "default",
			},
		},
	}

	tests := []struct {
		name              string
		phases            []types.Phase
		expectedConflicts int
	}{
		{
			name: "no duplicates",
			phases: []types.Phase{
				{
					Name:    "phase1",
					Objects: []unstructured.Unstructured{obj1},
				},
				{
					Name:    "phase2",
					Objects: []unstructured.Unstructured{obj2},
				},
			},
			expectedConflicts: 0,
		},
		{
			name: "duplicate across phases",
			phases: []types.Phase{
				{
					Name:    "phase1",
					Objects: []unstructured.Unstructured{obj1},
				},
				{
					Name:    "phase2",
					Objects: []unstructured.Unstructured{obj1},
				},
			},
			expectedConflicts: 1,
		},
		{
			name: "multiple duplicates",
			phases: []types.Phase{
				{
					Name:    "phase1",
					Objects: []unstructured.Unstructured{obj1, obj2},
				},
				{
					Name:    "phase2",
					Objects: []unstructured.Unstructured{obj1, obj2},
				},
			},
			expectedConflicts: 2,
		},
		{
			name: "duplicate in same phase",
			phases: []types.Phase{
				{
					Name:    "phase1",
					Objects: []unstructured.Unstructured{obj1, obj1},
				},
			},
			expectedConflicts: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			conflicts := checkForObjectDuplicates(test.phases...)

			assert.Len(t, conflicts, test.expectedConflicts)

			for _, conflict := range conflicts {
				assert.Len(t, conflict.Errors, 1)

				var dupErr PhaseObjectDuplicationError

				assert.ErrorAs(t, conflict.Errors[0], &dupErr)
			}
		})
	}
}

func TestCompactObjectViolations(t *testing.T) {
	t.Parallel()

	obj1Ref := types.ObjectRef{
		ObjectKey: client.ObjectKey{
			Name:      "obj1",
			Namespace: "default",
		},
	}

	obj2Ref := types.ObjectRef{
		ObjectKey: client.ObjectKey{
			Name:      "obj2",
			Namespace: "default",
		},
	}

	violations := []ObjectValidationError{
		{
			ObjectRef: obj1Ref,
			Errors:    []error{errors.New("error1")},
		},
		{
			ObjectRef: obj1Ref,
			Errors:    []error{errors.New("error2")},
		},
		{
			ObjectRef: obj2Ref,
			Errors:    []error{errors.New("error3")},
		},
	}

	compacted := compactObjectViolations(violations)

	assert.Len(t, compacted, 2)

	for _, compact := range compacted {
		switch compact.ObjectRef {
		case obj1Ref:
			assert.Len(t, compact.Errors, 2)
		case obj2Ref:
			assert.Len(t, compact.Errors, 1)
		}
	}
}
