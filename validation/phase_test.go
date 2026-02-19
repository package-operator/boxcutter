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
)

type mockObjectValidator struct {
	mock.Mock
}

func (m *mockObjectValidator) Validate(ctx context.Context, owner client.Object, obj client.Object) error {
	args := m.Called(ctx, owner, obj)

	return args.Error(0)
}

type testablePhaseValidator struct {
	validator        *PhaseValidator
	mockObjValidator *mockObjectValidator
}

func (v *testablePhaseValidator) Validate(ctx context.Context, owner client.Object, phase types.Phase) error {
	phaseError := validatePhaseName(phase)

	var (
		objectErrors []ObjectValidationError
		errs         []error
	)

	for _, obj := range phase.GetObjects() {
		err := v.mockObjValidator.Validate(ctx, owner, obj)
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

func TestNewClusterPhaseValidator(t *testing.T) {
	t.Parallel()

	restMapper := &mockRestMapper{}
	writer := &mockWriter{}

	validator := NewClusterPhaseValidator(restMapper, writer)

	assert.NotNil(t, validator)
	assert.NotNil(t, validator.ObjectValidator)
	assert.True(t, validator.allowNamespaceEscalation)
}

func TestNewNamespacedPhaseValidator(t *testing.T) {
	t.Parallel()

	restMapper := &mockRestMapper{}
	writer := &mockWriter{}

	validator := NewNamespacedPhaseValidator(restMapper, writer)

	assert.NotNil(t, validator)
	assert.NotNil(t, validator.ObjectValidator)
	assert.False(t, validator.allowNamespaceEscalation)
}

func TestPhaseValidator_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                     string
		phase                    types.Phase
		owner                    client.Object
		mockSetup                func(*mockObjectValidator)
		expectError              bool
		expectPhaseValidationErr bool
		useRealValidator         bool
	}{
		{
			name: "valid phase",
			phase: types.NewPhase(
				"valid-phase",
				[]client.Object{
					&unstructured.Unstructured{
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
			),
			owner: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"namespace": "default",
					},
				},
			},
			mockSetup: func(objValidator *mockObjectValidator) {
				objValidator.On("Validate", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectError: false,
		},
		{
			name: "invalid phase name",
			phase: types.NewPhase(
				"Invalid_Phase_Name",
				[]client.Object{
					&unstructured.Unstructured{
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
			),
			owner: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"namespace": "default",
					},
				},
			},
			mockSetup: func(objValidator *mockObjectValidator) {
				objValidator.On("Validate", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectError:              true,
			expectPhaseValidationErr: true,
		},
		{
			name: "object validation error from mock",
			phase: types.NewPhase(
				"valid-phase",
				[]client.Object{
					&unstructured.Unstructured{
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
			),
			owner: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"namespace": "default",
					},
				},
			},
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
			phase: types.NewPhase(
				"valid-phase",
				[]client.Object{
					&unstructured.Unstructured{
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
			),
			owner: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"namespace": "default",
					},
				},
			},
			mockSetup: func(objValidator *mockObjectValidator) {
				objValidator.On("Validate", mock.Anything, mock.Anything, mock.Anything).Return(
					errors.New("unknown error"))
			},
			expectError:              true,
			expectPhaseValidationErr: false,
		},
		{
			name: "duplicate objects in same phase",
			phase: types.NewPhase(
				"valid-phase",
				[]client.Object{
					&unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "ConfigMap",
							"metadata": map[string]interface{}{
								"name":      "test1",
								"namespace": "default",
							},
						},
					},
					&unstructured.Unstructured{
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
			),
			owner: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"namespace": "default",
					},
				},
			},
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

			if test.useRealValidator {
				restMapper := &mockRestMapper{}
				writer := &mockWriter{}

				restMapper.On("RESTMapping", mock.Anything, mock.Anything).Return(
					&meta.RESTMapping{Scope: meta.RESTScopeNamespace}, nil)
				writer.On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

				realValidator := NewClusterPhaseValidator(restMapper, writer)
				err = realValidator.Validate(t.Context(), test.phase, types.WithOwner(test.owner, nil))
			} else {
				objValidator = &mockObjectValidator{}
				test.mockSetup(objValidator)

				validator := &testablePhaseValidator{
					validator:        &PhaseValidator{ObjectValidator: &ObjectValidator{}},
					mockObjValidator: objValidator,
				}

				err = validator.Validate(t.Context(), test.owner, test.phase)
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
			name:        "valid phase name",
			phase:       types.NewPhase("valid-phase-name", nil),
			expectError: false,
		},
		{
			name:        "invalid phase name with uppercase",
			phase:       types.NewPhase("Invalid-Phase-Name", nil),
			expectError: true,
		},
		{
			name:        "invalid phase name with underscores",
			phase:       types.NewPhase("invalid_phase_name", nil),
			expectError: true,
		},
		{
			name:        "invalid phase name too long",
			phase:       types.NewPhase("this-is-a-very-long-phase-name-that-exceeds-the-dns1035-label-length-limit-of-63-characters", nil),
			expectError: true,
		},
		{
			name:        "empty phase name",
			phase:       types.NewPhase("", nil),
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
				types.NewPhase("phase1", []client.Object{&obj1}),
				types.NewPhase("phase2", []client.Object{&obj2}),
			},
			expectedConflicts: 0,
		},
		{
			name: "duplicate across phases",
			phases: []types.Phase{
				types.NewPhase("phase1", []client.Object{&obj1}),
				types.NewPhase("phase2", []client.Object{&obj1}),
			},
			expectedConflicts: 1,
		},
		{
			name: "multiple duplicates",
			phases: []types.Phase{
				types.NewPhase("phase1", []client.Object{&obj1, &obj2}),
				types.NewPhase("phase2", []client.Object{&obj1, &obj2}),
			},
			expectedConflicts: 2,
		},
		{
			name: "duplicate in same phase",
			phases: []types.Phase{
				types.NewPhase("phase1", []client.Object{&obj1, &obj1}),
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
