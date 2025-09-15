package validation

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	apimachineryerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type mockRestMapper struct {
	mock.Mock
}

func (m *mockRestMapper) RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	args := m.Called(gk, versions)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	return args.Get(0).(*meta.RESTMapping), args.Error(1)
}

type mockWriter struct {
	mock.Mock
}

func (m *mockWriter) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
	args := m.Called(ctx, obj, opts)

	return args.Error(0)
}

func (m *mockWriter) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	args := m.Called(ctx, obj, opts)

	return args.Error(0)
}

func (m *mockWriter) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	args := m.Called(ctx, obj, opts)

	return args.Error(0)
}

func (m *mockWriter) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	args := m.Called(ctx, obj, opts)

	return args.Error(0)
}

func (m *mockWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	args := m.Called(ctx, obj, patch, opts)

	return args.Error(0)
}

func (m *mockWriter) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	args := m.Called(ctx, obj, opts)

	return args.Error(0)
}

func TestNewClusterObjectValidator(t *testing.T) {
	t.Parallel()

	restMapper := &mockRestMapper{}
	writer := &mockWriter{}

	validator := NewClusterObjectValidator(restMapper, writer)

	assert.NotNil(t, validator)
	assert.True(t, validator.allowNamespaceEscalation)
	assert.Equal(t, restMapper, validator.restMapper)
	assert.Equal(t, writer, validator.writer)
}

func TestNewNamespacedObjectValidator(t *testing.T) {
	t.Parallel()

	restMapper := &mockRestMapper{}
	writer := &mockWriter{}

	validator := NewNamespacedObjectValidator(restMapper, writer)

	assert.NotNil(t, validator)
	assert.False(t, validator.allowNamespaceEscalation)
	assert.Equal(t, restMapper, validator.restMapper)
	assert.Equal(t, writer, validator.writer)
}

func TestObjectValidator_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                     string
		allowNamespaceEscalation bool
		obj                      *unstructured.Unstructured
		owner                    client.Object
		mockSetup                func(*mockRestMapper, *mockWriter)
		expectError              bool
		expectValidationError    bool
	}{
		{
			name:                     "valid object with cluster validator",
			allowNamespaceEscalation: true,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
				},
			},
			owner: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"namespace": "default",
					},
				},
			},
			mockSetup: func(_ *mockRestMapper, writer *mockWriter) {
				writer.On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectError:           false,
			expectValidationError: false,
		},
		{
			name:                     "valid object with namespaced validator",
			allowNamespaceEscalation: false,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
				},
			},
			owner: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"namespace": "default",
					},
				},
			},
			mockSetup: func(restMapper *mockRestMapper, writer *mockWriter) {
				restMapper.On("RESTMapping", schema.GroupKind{Group: "", Kind: "ConfigMap"}, []string{"v1"}).Return(
					&meta.RESTMapping{Scope: meta.RESTScopeNamespace}, nil)
				writer.On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectError:           false,
			expectValidationError: false,
		},
		{
			name:                     "namespace validation fails",
			allowNamespaceEscalation: false,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "other",
					},
				},
			},
			owner: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"namespace": "default",
					},
				},
			},
			mockSetup: func(restMapper *mockRestMapper, _ *mockWriter) {
				restMapper.On("RESTMapping", schema.GroupKind{Group: "", Kind: "ConfigMap"}, []string{"v1"}).Return(
					&meta.RESTMapping{Scope: meta.RESTScopeNamespace}, nil)
			},
			expectError:           true,
			expectValidationError: true,
		},
		{
			name:                     "dry run validation fails",
			allowNamespaceEscalation: true,
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
				},
			},
			owner: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"namespace": "default",
					},
				},
			},
			mockSetup: func(_ *mockRestMapper, writer *mockWriter) {
				statusErr := &apimachineryerrors.StatusError{
					ErrStatus: metav1.Status{
						Reason: metav1.StatusReasonInvalid,
					},
				}
				writer.On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(statusErr)
			},
			expectError:           true,
			expectValidationError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			restMapper := &mockRestMapper{}
			writer := &mockWriter{}
			test.mockSetup(restMapper, writer)

			validator := &ObjectValidator{
				restMapper:               restMapper,
				writer:                   writer,
				allowNamespaceEscalation: test.allowNamespaceEscalation,
			}

			err := validator.Validate(t.Context(), test.owner, test.obj)

			if test.expectError {
				require.Error(t, err)

				if test.expectValidationError {
					var objErr *ObjectValidationError

					require.ErrorAs(t, err, &objErr)
				}
			} else {
				require.NoError(t, err)
			}

			restMapper.AssertExpectations(t)
			writer.AssertExpectations(t)
		})
	}
}

func TestMustBeNamespaceScopedResourceError(t *testing.T) {
	t.Parallel()

	err := MustBeNamespaceScopedResourceError{}
	assert.Equal(t, "object must be namespace-scoped", err.Error())
}

func TestMustBeInNamespaceError(t *testing.T) {
	t.Parallel()

	err := MustBeInNamespaceError{
		ExpectedNamespace: "default",
		ActualNamespace:   "other",
	}
	assert.Equal(t, "object must be in namespace \"default\", actual \"other\"", err.Error())
}

func TestValidateNamespace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		namespace     string
		obj           *unstructured.Unstructured
		mockSetup     func(*mockRestMapper)
		expectedError error
		expectNoError bool
	}{
		{
			name:      "no namespace limitation",
			namespace: "",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "any",
					},
				},
			},
			mockSetup:     func(_ *mockRestMapper) {},
			expectNoError: true,
		},
		{
			name:      "cluster-scoped resource",
			namespace: "default",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Namespace",
					"metadata": map[string]interface{}{
						"name": "test",
					},
				},
			},
			mockSetup: func(restMapper *mockRestMapper) {
				restMapper.On("RESTMapping", schema.GroupKind{Group: "", Kind: "Namespace"}, []string{"v1"}).Return(
					&meta.RESTMapping{Scope: meta.RESTScopeRoot}, nil)
			},
			expectedError: MustBeNamespaceScopedResourceError{},
		},
		{
			name:      "namespace-scoped resource in correct namespace",
			namespace: "default",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
				},
			},
			mockSetup: func(restMapper *mockRestMapper) {
				restMapper.On("RESTMapping", schema.GroupKind{Group: "", Kind: "ConfigMap"}, []string{"v1"}).Return(
					&meta.RESTMapping{Scope: meta.RESTScopeNamespace}, nil)
			},
			expectNoError: true,
		},
		{
			name:      "namespace-scoped resource in wrong namespace",
			namespace: "default",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "other",
					},
				},
			},
			mockSetup: func(restMapper *mockRestMapper) {
				restMapper.On("RESTMapping", schema.GroupKind{Group: "", Kind: "ConfigMap"}, []string{"v1"}).Return(
					&meta.RESTMapping{Scope: meta.RESTScopeNamespace}, nil)
			},
			expectedError: MustBeInNamespaceError{
				ExpectedNamespace: "default",
				ActualNamespace:   "other",
			},
		},
		{
			name:      "API does not exist",
			namespace: "default",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "example.com/v1",
					"kind":       "Unknown",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
				},
			},
			mockSetup: func(restMapper *mockRestMapper) {
				restMapper.On("RESTMapping", schema.GroupKind{Group: "example.com", Kind: "Unknown"}, []string{"v1"}).Return(
					nil, &meta.NoKindMatchError{})
			},
			expectedError: &meta.NoKindMatchError{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			restMapper := &mockRestMapper{}
			test.mockSetup(restMapper)

			err := validateNamespace(restMapper, test.namespace, test.obj)

			if test.expectNoError {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Equal(t, test.expectedError.Error(), err.Error())
			}

			restMapper.AssertExpectations(t)
		})
	}
}

func TestDryRunValidationError(t *testing.T) {
	t.Parallel()

	innerErr := errors.New("inner error")
	err := DryRunValidationError{err: innerErr}

	assert.Equal(t, "inner error", err.Error())
	assert.Equal(t, innerErr, err.Unwrap())
}

func TestValidateDryRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		obj                  *unstructured.Unstructured
		mockSetup            func(*mockWriter)
		expectError          bool
		expectDryRunValError bool
	}{
		{
			name: "successful dry run",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
				},
			},
			mockSetup: func(writer *mockWriter) {
				writer.On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectError: false,
		},
		{
			name: "not found, create succeeds",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
				},
			},
			mockSetup: func(writer *mockWriter) {
				writer.On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(
					apimachineryerrors.NewNotFound(schema.GroupResource{}, "test"))
				writer.On("Create", mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectError: false,
		},
		{
			name: "validation error",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
				},
			},
			mockSetup: func(writer *mockWriter) {
				statusErr := &apimachineryerrors.StatusError{
					ErrStatus: metav1.Status{
						Reason: metav1.StatusReasonInvalid,
					},
				}
				writer.On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(statusErr)
			},
			expectError:          true,
			expectDryRunValError: true,
		},
		{
			name: "unauthorized error",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
				},
			},
			mockSetup: func(writer *mockWriter) {
				statusErr := &apimachineryerrors.StatusError{
					ErrStatus: metav1.Status{
						Reason: metav1.StatusReasonUnauthorized,
					},
				}
				writer.On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(statusErr)
			},
			expectError:          true,
			expectDryRunValError: true,
		},
		{
			name: "empty reason with typed patch error",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
				},
			},
			mockSetup: func(writer *mockWriter) {
				statusErr := &apimachineryerrors.StatusError{
					ErrStatus: metav1.Status{
						Reason:  "",
						Message: "failed to create typed patch object",
					},
				}
				writer.On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(statusErr)
			},
			expectError:          true,
			expectDryRunValError: true,
		},
		{
			name: "non-API status error",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      "test",
						"namespace": "default",
					},
				},
			},
			mockSetup: func(writer *mockWriter) {
				writer.On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(
					errors.New("network error"))
			},
			expectError:          true,
			expectDryRunValError: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			writer := &mockWriter{}
			test.mockSetup(writer)

			err := validateDryRun(t.Context(), writer, test.obj)

			if test.expectError {
				require.Error(t, err)

				if test.expectDryRunValError {
					var drvErr DryRunValidationError

					require.ErrorAs(t, err, &drvErr)
				}
			} else {
				require.NoError(t, err)
			}

			writer.AssertExpectations(t)
		})
	}
}
