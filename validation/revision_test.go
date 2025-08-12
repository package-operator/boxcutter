package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"pkg.package-operator.run/boxcutter/machinery/types"
)

func TestNewRevisionValidator(t *testing.T) {
	t.Parallel()

	validator := NewRevisionValidator()

	assert.NotNil(t, validator)
}

func TestRevisionValidator_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                        string
		revision                    types.Revision
		expectError                 bool
		expectRevisionValidationErr bool
	}{
		{
			name: "valid revision",
			revision: types.Revision{
				Name:     "test-revision",
				Revision: 1,
				Phases: []types.Phase{
					{
						Name: "phase1",
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
					{
						Name: "phase2",
						Objects: []unstructured.Unstructured{
							{
								Object: map[string]interface{}{
									"apiVersion": "v1",
									"kind":       "ConfigMap",
									"metadata": map[string]interface{}{
										"name":      "test2",
										"namespace": "default",
									},
								},
							},
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "revision with invalid phase name",
			revision: types.Revision{
				Name:     "test-revision",
				Revision: 1,
				Phases: []types.Phase{
					{
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
				},
			},
			expectError:                 true,
			expectRevisionValidationErr: true,
		},
		{
			name: "revision with metadata validation errors",
			revision: types.Revision{
				Name:     "test-revision",
				Revision: 1,
				Phases: []types.Phase{
					{
						Name: "phase1",
						Objects: []unstructured.Unstructured{
							{
								Object: map[string]interface{}{
									"kind": "ConfigMap",
									"metadata": map[string]interface{}{
										"name":      "test1",
										"namespace": "default",
									},
								},
							},
						},
					},
				},
			},
			expectError:                 true,
			expectRevisionValidationErr: true,
		},
		{
			name: "revision with duplicate objects across phases",
			revision: types.Revision{
				Name:     "test-revision",
				Revision: 1,
				Phases: []types.Phase{
					{
						Name: "phase1",
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
					{
						Name: "phase2",
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
				},
			},
			// Duplicate detection now works properly
			expectError:                 true,
			expectRevisionValidationErr: true,
		},
		{
			name: "empty revision",
			revision: types.Revision{
				Name:     "test-revision",
				Revision: 1,
				Phases:   []types.Phase{},
			},
			expectError: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			validator := NewRevisionValidator()

			err := validator.Validate(t.Context(), test.revision)

			if test.expectError {
				require.Error(t, err)

				if test.expectRevisionValidationErr {
					var revErr *RevisionValidationError

					require.ErrorAs(t, err, &revErr)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestStaticValidateMultiplePhases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                    string
		phases                  []types.Phase
		expectedPhaseErrorCount int
	}{
		{
			name: "valid phases",
			phases: []types.Phase{
				{
					Name: "phase1",
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
				{
					Name: "phase2",
					Objects: []unstructured.Unstructured{
						{
							Object: map[string]interface{}{
								"apiVersion": "v1",
								"kind":       "ConfigMap",
								"metadata": map[string]interface{}{
									"name":      "test2",
									"namespace": "default",
								},
							},
						},
					},
				},
			},
			expectedPhaseErrorCount: 0,
		},
		{
			name: "phase with invalid name",
			phases: []types.Phase{
				{
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
			},
			expectedPhaseErrorCount: 1,
		},
		{
			name: "phase with metadata validation errors",
			phases: []types.Phase{
				{
					Name: "valid-phase",
					Objects: []unstructured.Unstructured{
						{
							Object: map[string]interface{}{
								"kind": "ConfigMap",
								"metadata": map[string]interface{}{
									"name":      "test1",
									"namespace": "default",
								},
							},
						},
					},
				},
			},
			expectedPhaseErrorCount: 1,
		},
		{
			name: "phases with duplicate objects",
			phases: []types.Phase{
				{
					Name: "phase1",
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
				{
					Name: "phase2",
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
			},
			expectedPhaseErrorCount: 2,
		},
		{
			name: "multiple issues",
			phases: []types.Phase{
				{
					Name: "Invalid_Phase_Name",
					Objects: []unstructured.Unstructured{
						{
							Object: map[string]interface{}{
								"kind": "ConfigMap",
								"metadata": map[string]interface{}{
									"name":      "test1",
									"namespace": "default",
								},
							},
						},
					},
				},
				{
					Name: "phase2",
					Objects: []unstructured.Unstructured{
						{
							Object: map[string]interface{}{
								"apiVersion": "v1",
								"kind":       "ConfigMap",
								"metadata": map[string]interface{}{
									"name":      "test2",
									"namespace": "default",
									"uid":       "some-uid",
								},
							},
						},
					},
				},
			},
			expectedPhaseErrorCount: 2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			phaseErrors := staticValidateMultiplePhases(test.phases...)

			assert.Len(t, phaseErrors, test.expectedPhaseErrorCount)

			for _, phaseErr := range phaseErrors {
				assert.NotEmpty(t, phaseErr.PhaseName)
				assert.True(t, phaseErr.PhaseError != nil || len(phaseErr.Objects) > 0)
			}
		})
	}
}

func TestStaticValidateMultiplePhases_DuplicateHandling(t *testing.T) {
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

	phases := []types.Phase{
		{
			Name:    "phase1",
			Objects: []unstructured.Unstructured{obj1},
		},
		{
			Name:    "phase2",
			Objects: []unstructured.Unstructured{obj1},
		},
	}

	phaseErrors := staticValidateMultiplePhases(phases...)

	assert.Len(t, phaseErrors, 2)
}

func TestStaticValidateMultiplePhases_EmptyPhases(t *testing.T) {
	t.Parallel()

	phaseErrors := staticValidateMultiplePhases()

	assert.Empty(t, phaseErrors)
}

func TestStaticValidateMultiplePhases_PhaseWithoutErrors(t *testing.T) {
	t.Parallel()

	validPhase := types.Phase{
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
	}

	phaseErrors := staticValidateMultiplePhases(validPhase)

	assert.Empty(t, phaseErrors)
}
