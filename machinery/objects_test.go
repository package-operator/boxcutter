package machinery

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"pkg.package-operator.run/boxcutter/internal/testutil"
	"pkg.package-operator.run/boxcutter/machinery/ownerhandling"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/structured-merge-diff/v4/fieldpath"
	"sigs.k8s.io/structured-merge-diff/v4/typed"
)

const (
	testSystemPrefix = "testtest.xxx"
)

//nolint:maintidx
func TestObjectEngine(t *testing.T) {
	t.Parallel()
	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "12345-678",
			Name:      "owner",
			Namespace: "test",
		},
	}

	oldOwner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "6789",
			Name:      "old-owner",
			Namespace: "test",
		},
	}

	tests := []struct {
		name          string
		revision      int64
		desiredObject *unstructured.Unstructured
		opts          []ObjectOption

		mockSetup func(
			*cacheMock,
			*testutil.CtrlClient,
			*divergeDetectorMock,
		)

		expectedAction Action
		expectedObject *unstructured.Unstructured
	}{
		{
			name:     "Updated noController CollisionProtectionIfNoController",
			revision: 1,
			desiredObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "testi",
						"namespace": "test",
					},
				},
			},
			opts: []ObjectOption{
				WithCollisionProtection(CollisionProtectionIfNoController),
			},

			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *divergeDetectorMock,
			) {
				actualObject := &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Secret",
						"metadata": map[string]interface{}{
							"name":      "testi",
							"namespace": "test",
						},
					},
				}

				// Mock setup
				cache.
					On(
						"Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything,
					).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*unstructured.Unstructured)
						*obj = *actualObject
					}).
					Return(nil)
				ddm.
					On("HasDiverged", owner, mock.Anything, mock.Anything).
					Return(DivergeResult{}, nil)

				writer.
					On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(nil)
			},

			expectedAction: ActionUpdated,
			expectedObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "testi",
						"namespace": "test",
						"ownerReferences": []interface{}{
							map[string]interface{}{
								"apiVersion":         "v1",
								"kind":               "ConfigMap",
								"controller":         true,
								"name":               "owner",
								"uid":                "12345-678",
								"blockOwnerDeletion": true,
							},
						},
						"annotations": map[string]interface{}{
							"testtest.xxx/revision": "1",
						},
					},
				},
			},
		},
		{
			name:     "Created",
			revision: 1,
			desiredObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "testi",
						"namespace": "test",
					},
				},
			},

			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *divergeDetectorMock,
			) {
				// Mock setup
				cache.
					On(
						"Get", mock.Anything,
						client.ObjectKey{
							Name:      "testi",
							Namespace: "test",
						},
						mock.Anything, mock.Anything,
					).
					Return(errors.NewNotFound(schema.GroupResource{}, ""))
				ddm.
					On("HasDiverged", owner, mock.Anything, mock.Anything).
					Return(DivergeResult{}, nil)

				writer.
					On("Create", mock.Anything, mock.Anything, mock.Anything).
					Return(nil)
			},

			expectedAction: ActionCreated,
			expectedObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "testi",
						"namespace": "test",
						"ownerReferences": []interface{}{
							map[string]interface{}{
								"apiVersion":         "v1",
								"kind":               "ConfigMap",
								"controller":         true,
								"name":               "owner",
								"uid":                "12345-678",
								"blockOwnerDeletion": true,
							},
						},
						"annotations": map[string]interface{}{
							"testtest.xxx/revision": "1",
						},
					},
				},
			},
		},
		{
			name:     "Progressed",
			revision: 1,
			desiredObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "testi",
						"namespace": "test",
					},
				},
			},
			opts: []ObjectOption{},

			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *divergeDetectorMock,
			) {
				actualObject := &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Secret",
						"metadata": map[string]interface{}{
							"name":      "testi",
							"namespace": "test",
							"annotations": map[string]interface{}{
								"testtest.xxx/revision": "4",
							},
						},
					},
				}

				// Mock setup
				cache.
					On(
						"Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything,
					).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*unstructured.Unstructured)
						*obj = *actualObject
					}).
					Return(nil)
				ddm.
					On("HasDiverged", owner, mock.Anything, mock.Anything).
					Return(DivergeResult{}, nil)

				writer.
					On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(nil)
			},

			expectedAction: ActionProgressed,
			expectedObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "testi",
						"namespace": "test",
						"annotations": map[string]interface{}{
							"testtest.xxx/revision": "4",
						},
					},
				},
			},
		},
		{
			name:     "Idle",
			revision: 1,
			desiredObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "testi",
						"namespace": "test",
					},
				},
			},
			opts: []ObjectOption{},

			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *divergeDetectorMock,
			) {
				actualObject := &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Secret",
						"metadata": map[string]interface{}{
							"name":      "testi",
							"namespace": "test",
							"annotations": map[string]interface{}{
								"testtest.xxx/revision": "1",
							},
							"ownerReferences": []interface{}{
								map[string]interface{}{
									"apiVersion":         "v1",
									"kind":               "ConfigMap",
									"controller":         true,
									"name":               "owner",
									"uid":                "12345-678",
									"blockOwnerDeletion": true,
								},
							},
						},
					},
				}

				// Mock setup
				cache.
					On(
						"Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything,
					).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*unstructured.Unstructured)
						*obj = *actualObject
					}).
					Return(nil)
				ddm.
					On("HasDiverged", owner, mock.Anything, mock.Anything).
					Return(DivergeResult{}, nil)

				writer.
					On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(nil)
			},

			expectedAction: ActionIdle,
			expectedObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "testi",
						"namespace": "test",
						"annotations": map[string]interface{}{
							"testtest.xxx/revision": "1",
						},
						"ownerReferences": []interface{}{
							map[string]interface{}{
								"apiVersion":         "v1",
								"kind":               "ConfigMap",
								"controller":         true,
								"name":               "owner",
								"uid":                "12345-678",
								"blockOwnerDeletion": true,
							},
						},
					},
				},
			},
		},
		{
			name:     "Updated - modified",
			revision: 1,
			desiredObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "testi",
						"namespace": "test",
					},
				},
			},
			opts: []ObjectOption{},

			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *divergeDetectorMock,
			) {
				actualObject := &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Secret",
						"metadata": map[string]interface{}{
							"name":      "testi",
							"namespace": "test",
							"annotations": map[string]interface{}{
								"testtest.xxx/revision": "1",
							},
							"ownerReferences": []interface{}{
								map[string]interface{}{
									"apiVersion":         "v1",
									"kind":               "ConfigMap",
									"controller":         true,
									"name":               "owner",
									"uid":                "12345-678",
									"blockOwnerDeletion": true,
								},
							},
						},
					},
				}

				// Mock setup
				cache.
					On(
						"Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything,
					).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*unstructured.Unstructured)
						*obj = *actualObject
					}).
					Return(nil)
				fs := &fieldpath.Set{}
				fs.Insert(fieldpath.MakePathOrDie("spec", "banana"))
				ddm.
					On("HasDiverged", owner, mock.Anything, mock.Anything).
					Return(DivergeResult{
						Comparison: &typed.Comparison{
							Added:    &fieldpath.Set{},
							Removed:  &fieldpath.Set{},
							Modified: fs,
						},
					}, nil)

				writer.
					On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(nil)
			},

			expectedAction: ActionUpdated,
			expectedObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "testi",
						"namespace": "test",
						"annotations": map[string]interface{}{
							"testtest.xxx/revision": "1",
						},
						"ownerReferences": []interface{}{
							map[string]interface{}{
								"apiVersion":         "v1",
								"kind":               "ConfigMap",
								"controller":         true,
								"name":               "owner",
								"uid":                "12345-678",
								"blockOwnerDeletion": true,
							},
						},
					},
				},
			},
		},
		{
			name:     "Recovered",
			revision: 1,
			desiredObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "testi",
						"namespace": "test",
					},
				},
			},
			opts: []ObjectOption{},

			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *divergeDetectorMock,
			) {
				actualObject := &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Secret",
						"metadata": map[string]interface{}{
							"name":      "testi",
							"namespace": "test",
							"annotations": map[string]interface{}{
								"testtest.xxx/revision": "1",
							},
							"ownerReferences": []interface{}{
								map[string]interface{}{
									"apiVersion":         "v1",
									"kind":               "ConfigMap",
									"controller":         true,
									"name":               "owner",
									"uid":                "12345-678",
									"blockOwnerDeletion": true,
								},
							},
						},
					},
				}

				// Mock setup
				cache.
					On(
						"Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything,
					).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*unstructured.Unstructured)
						*obj = *actualObject
					}).
					Return(nil)
				fs := &fieldpath.Set{}
				fs.Insert(fieldpath.MakePathOrDie("spec", "banana"))
				ddm.
					On("HasDiverged", owner, mock.Anything, mock.Anything).
					Return(DivergeResult{
						ConflictingFieldOwners: []string{"xxx"},
					}, nil)

				writer.
					On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(nil)
			},

			expectedAction: ActionRecovered,
			expectedObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "testi",
						"namespace": "test",
						"annotations": map[string]interface{}{
							"testtest.xxx/revision": "1",
						},
						"ownerReferences": []interface{}{
							map[string]interface{}{
								"apiVersion":         "v1",
								"kind":               "ConfigMap",
								"controller":         true,
								"name":               "owner",
								"uid":                "12345-678",
								"blockOwnerDeletion": true,
							},
						},
					},
				},
			},
		},
		{
			name:     "Collision - unknown controller",
			revision: 1,
			desiredObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "testi",
						"namespace": "test",
					},
				},
			},
			opts: []ObjectOption{},

			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *divergeDetectorMock,
			) {
				actualObject := &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Secret",
						"metadata": map[string]interface{}{
							"name":      "testi",
							"namespace": "test",
							"annotations": map[string]interface{}{
								"testtest.xxx/revision": "1",
							},
							"ownerReferences": []interface{}{
								map[string]interface{}{
									"apiVersion":         "v1",
									"kind":               "Node",
									"controller":         true,
									"name":               "node1",
									"uid":                "xxxx",
									"blockOwnerDeletion": true,
								},
							},
						},
					},
				}

				// Mock setup
				cache.
					On(
						"Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything,
					).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*unstructured.Unstructured)
						*obj = *actualObject
					}).
					Return(nil)
				fs := &fieldpath.Set{}
				fs.Insert(fieldpath.MakePathOrDie("spec", "banana"))
				ddm.
					On("HasDiverged", owner, mock.Anything, mock.Anything).
					Return(DivergeResult{}, nil)

				writer.
					On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(nil)
			},

			expectedAction: ActionCollision,
			expectedObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "testi",
						"namespace": "test",
						"annotations": map[string]interface{}{
							"testtest.xxx/revision": "1",
						},
						"ownerReferences": []interface{}{
							map[string]interface{}{
								"apiVersion":         "v1",
								"kind":               "Node",
								"controller":         true,
								"name":               "node1",
								"uid":                "xxxx",
								"blockOwnerDeletion": true,
							},
						},
					},
				},
			},
		},
		{
			name:     "Updated takeover from previousOwner",
			revision: 1,
			desiredObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "testi",
						"namespace": "test",
					},
				},
			},
			opts: []ObjectOption{
				WithPreviousOwners{oldOwner},
			},

			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *divergeDetectorMock,
			) {
				actualObject := &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Secret",
						"metadata": map[string]interface{}{
							"name":      "testi",
							"namespace": "test",
							"annotations": map[string]interface{}{
								"testtest.xxx/revision": "1",
							},
							"ownerReferences": []interface{}{
								map[string]interface{}{
									"apiVersion":         "v1",
									"kind":               "ConfigMap",
									"controller":         true,
									"name":               "old-owner",
									"uid":                "6789",
									"blockOwnerDeletion": true,
								},
							},
						},
					},
				}

				// Mock setup
				cache.
					On(
						"Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything,
					).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*unstructured.Unstructured)
						*obj = *actualObject
					}).
					Return(nil)
				fs := &fieldpath.Set{}
				fs.Insert(fieldpath.MakePathOrDie("spec", "banana"))
				ddm.
					On("HasDiverged", owner, mock.Anything, mock.Anything).
					Return(DivergeResult{}, nil)

				writer.
					On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(nil)
			},

			expectedAction: ActionUpdated,
			expectedObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "testi",
						"namespace": "test",
						"annotations": map[string]interface{}{
							"testtest.xxx/revision": "1",
						},
						"ownerReferences": []interface{}{
							map[string]interface{}{
								"apiVersion":         "v1",
								"kind":               "ConfigMap",
								"controller":         false,
								"name":               "old-owner",
								"uid":                "6789",
								"blockOwnerDeletion": true,
							},
							map[string]interface{}{
								"apiVersion":         "v1",
								"kind":               "ConfigMap",
								"controller":         true,
								"name":               "owner",
								"uid":                "12345-678",
								"blockOwnerDeletion": true,
							},
						},
					},
				},
			},
		},
		{
			name:     "Collision - no controller",
			revision: 1,
			desiredObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "testi",
						"namespace": "test",
					},
				},
			},

			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *divergeDetectorMock,
			) {
				actualObject := &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Secret",
						"metadata": map[string]interface{}{
							"name":      "testi",
							"namespace": "test",
							"annotations": map[string]interface{}{
								"testtest.xxx/revision": "1",
							},
						},
					},
				}

				// Mock setup
				cache.
					On(
						"Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything,
					).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*unstructured.Unstructured)
						*obj = *actualObject
					}).
					Return(nil)
				fs := &fieldpath.Set{}
				fs.Insert(fieldpath.MakePathOrDie("spec", "banana"))
				ddm.
					On("HasDiverged", owner, mock.Anything, mock.Anything).
					Return(DivergeResult{}, nil)

				writer.
					On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(nil)
			},

			expectedAction: ActionCollision,
			expectedObject: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "testi",
						"namespace": "test",
						"annotations": map[string]interface{}{
							"testtest.xxx/revision": "1",
						},
					},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cache := &cacheMock{}
			writer := testutil.NewClient()
			ownerStrategy := ownerhandling.NewNative(scheme.Scheme)
			divergeDetector := &divergeDetectorMock{}

			oe := NewObjectEngine(
				cache, writer,
				ownerStrategy, divergeDetector,
				testFieldOwner,
				testSystemPrefix,
			)

			cache.
				On("Watch", mock.Anything, mock.Anything, mock.Anything).
				Return(nil)

			test.mockSetup(cache, writer, divergeDetector)

			ctx := context.Background()
			res, err := oe.Reconcile(
				ctx, owner, 1, test.desiredObject,
				test.opts...,
			)
			require.NoError(t, err)

			switch r := res.(type) {
			case ResultCreated:
				assert.Equal(t, test.expectedObject, r.Object())
			case ResultUpdated:
				assert.Equal(t, test.expectedObject, r.Object())
			case ResultIdle:
				assert.Equal(t, test.expectedObject, r.Object())
			case ResultProgressed:
				assert.Equal(t, test.expectedObject, r.Object())
			case ResultRecovered:
				assert.Equal(t, test.expectedObject, r.Object())
			}
			assert.Equal(t, test.expectedAction, res.Action())
		})
	}
}

func TestObjectEngine_SanityChecks(t *testing.T) {
	t.Parallel()
	oe := &ObjectEngine{}
	owner := &unstructured.Unstructured{}
	desired := &unstructured.Unstructured{}

	t.Run("missing revision", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, "owner revision must be set and start at 1", func() {
			_, _ = oe.Reconcile(context.Background(), owner, 0, desired)
		})
	})

	t.Run("missing owner.UID", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, "owner must be persistet to cluster, empty UID", func() {
			_, _ = oe.Reconcile(context.Background(), owner, 1, desired)
		})
	})
}

type cacheMock struct {
	testutil.CtrlClient
}

func (m *cacheMock) Watch(
	ctx context.Context,
	owner client.Object,
	obj runtime.Object,
) error {
	args := m.Called(ctx, owner, obj)
	return args.Error(0)
}

type divergeDetectorMock struct {
	mock.Mock
}

func (m *divergeDetectorMock) HasDiverged(
	owner client.Object,
	desiredObject, actualObject *unstructured.Unstructured,
) (DivergeResult, error) {
	args := m.Called(owner, desiredObject, actualObject)
	return args.Get(0).(DivergeResult), args.Error(1)
}
