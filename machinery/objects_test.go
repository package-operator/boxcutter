package machinery

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"
	"sigs.k8s.io/structured-merge-diff/v6/typed"

	"pkg.package-operator.run/boxcutter/internal/testutil"
	"pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/ownerhandling"
)

const (
	testSystemPrefix = "testtest.xxx"
)

//nolint:maintidx,dupl
func TestObjectEngine(t *testing.T) {
	t.Parallel()

	ownerStrat := ownerhandling.NewNative(scheme.Scheme)

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
		opts          []types.ObjectReconcileOption

		mockSetup func(
			*cacheMock,
			*testutil.CtrlClient,
			*comparatorMock,
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
			opts: []types.ObjectReconcileOption{
				types.WithOwner(owner, ownerStrat),
				types.WithCollisionProtection(types.CollisionProtectionIfNoController),
			},

			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *comparatorMock,
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
					On("Compare", mock.Anything, mock.Anything, mock.Anything).
					Return(CompareResult{}, nil)

				writer.
					On("Apply", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
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
						"labels": map[string]interface{}{
							"boxcutter-managed": "True",
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
			opts: []types.ObjectReconcileOption{
				types.WithOwner(owner, ownerStrat),
			},

			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *comparatorMock,
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
					Return(apierrors.NewNotFound(schema.GroupResource{}, ""))
				ddm.
					On("Compare", mock.Anything, mock.Anything, mock.Anything).
					Return(CompareResult{}, nil)

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
						"labels": map[string]interface{}{
							"boxcutter-managed": "True",
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
			opts: []types.ObjectReconcileOption{
				types.WithOwner(owner, ownerStrat),
			},

			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *comparatorMock,
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
					On("Compare", mock.Anything, mock.Anything, mock.Anything).
					Return(CompareResult{}, nil)

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
			opts: []types.ObjectReconcileOption{
				types.WithOwner(owner, ownerStrat),
			},

			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *comparatorMock,
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
					On("Compare", mock.Anything, mock.Anything, mock.Anything).
					Return(CompareResult{}, nil)

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
			opts: []types.ObjectReconcileOption{
				types.WithOwner(owner, ownerStrat),
			},

			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *comparatorMock,
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
					On("Compare", mock.Anything, mock.Anything, mock.Anything).
					Return(CompareResult{
						Comparison: &typed.Comparison{
							Added:    &fieldpath.Set{},
							Removed:  &fieldpath.Set{},
							Modified: fs,
						},
					}, nil)

				writer.
					On("Apply", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
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
						"labels": map[string]interface{}{
							"boxcutter-managed": "True",
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
			opts: []types.ObjectReconcileOption{
				types.WithOwner(owner, ownerStrat),
			},

			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *comparatorMock,
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
					On("Compare", mock.Anything, mock.Anything, mock.Anything).
					Return(CompareResult{
						ConflictingMangers: []CompareResultManagedFields{
							{Manager: "xxx"},
						},
					}, nil)

				writer.
					On("Apply", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
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
						"labels": map[string]interface{}{
							"boxcutter-managed": "True",
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
			opts: []types.ObjectReconcileOption{
				types.WithOwner(owner, ownerStrat),
			},

			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *comparatorMock,
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
					On("Compare", mock.Anything, mock.Anything, mock.Anything).
					Return(CompareResult{}, nil)

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
			opts: []types.ObjectReconcileOption{
				types.WithPreviousOwners{oldOwner},
				types.WithOwner(owner, ownerStrat),
			},

			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *comparatorMock,
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
					On("Compare", mock.Anything, mock.Anything, mock.Anything).
					Return(CompareResult{}, nil)

				writer.
					On("Apply", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
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
						"labels": map[string]interface{}{
							"boxcutter-managed": "True",
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
			opts: []types.ObjectReconcileOption{
				types.WithOwner(owner, ownerStrat),
			},

			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *comparatorMock,
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
					On("Compare", mock.Anything, mock.Anything, mock.Anything).
					Return(CompareResult{}, nil)

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
			divergeDetector := &comparatorMock{}

			oe := NewObjectEngine(
				scheme.Scheme,
				cache, writer,
				divergeDetector,
				testFieldOwner,
				testSystemPrefix,
			)

			test.mockSetup(cache, writer, divergeDetector)

			ctx := context.Background()
			res, err := oe.Reconcile(
				ctx, 1, test.desiredObject,
				test.opts...,
			)
			require.NoError(t, err)

			switch r := res.(type) {
			case ObjectResultCreated:
				assert.Equal(t, test.expectedObject, r.Object())
			case ObjectResultUpdated:
				assert.Equal(t, test.expectedObject, r.Object())
			case ObjectResultIdle:
				assert.Equal(t, test.expectedObject, r.Object())
			case ObjectResultProgressed:
				assert.Equal(t, test.expectedObject, r.Object())
			case ObjectResultRecovered:
				assert.Equal(t, test.expectedObject, r.Object())
			}

			assert.Equal(t, test.expectedAction, res.Action())
		})
	}
}

func TestObjectEngine_Reconcile_SanityChecks(t *testing.T) {
	t.Parallel()

	oe := &ObjectEngine{}
	owner := &unstructured.Unstructured{}
	desired := &unstructured.Unstructured{}

	t.Run("missing revision", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, "owner revision must be set and start at 1", func() {
			_, _ = oe.Reconcile(t.Context(), 0, desired)
		})
	})

	t.Run("missing owner.UID", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, "owner must be persisted to cluster, empty UID", func() {
			_, _ = oe.Reconcile(t.Context(), 1, desired, types.WithOwner(owner, nil))
		})
	})
}

func TestObjectEngine_Reconcile_UnsupportedTypedObject(t *testing.T) {
	t.Parallel()

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "12345-678",
			Name:      "owner",
			Namespace: "test",
		},
	}

	t.Run("returns error when updating typed object", func(t *testing.T) {
		t.Parallel()

		cache := &cacheMock{}
		writer := testutil.NewClient()
		ownerStrategy := ownerhandling.NewNative(scheme.Scheme)
		divergeDetector := &comparatorMock{}

		oe := NewObjectEngine(
			scheme.Scheme,
			cache, writer,
			divergeDetector,
			testFieldOwner,
			testSystemPrefix,
		)

		// Use a typed object (not unstructured)
		desiredObject := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-secret",
				Namespace: "test",
			},
		}

		// Actual object exists, so we'll hit the update path
		actualObject := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-secret",
				Namespace: "test",
				Annotations: map[string]string{
					testSystemPrefix + "/revision": "1",
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion:         "v1",
						Kind:               "ConfigMap",
						Controller:         ptr.To(true),
						Name:               "owner",
						UID:                "12345-678",
						BlockOwnerDeletion: ptr.To(true),
					},
				},
			},
		}

		// Mock setup
		cache.
			On(
				"Get", mock.Anything,
				client.ObjectKeyFromObject(desiredObject),
				mock.Anything, mock.Anything,
			).
			Run(func(args mock.Arguments) {
				obj := args.Get(2).(*corev1.Secret)
				*obj = *actualObject
			}).
			Return(nil)

		fs := &fieldpath.Set{}
		fs.Insert(fieldpath.MakePathOrDie("data", "key"))
		divergeDetector.
			On("Compare", mock.Anything, mock.Anything, mock.Anything).
			Return(CompareResult{
				Comparison: &typed.Comparison{
					Added:    &fieldpath.Set{},
					Removed:  &fieldpath.Set{},
					Modified: fs,
				},
			}, nil)

		ctx := context.Background()
		res, err := oe.Reconcile(ctx, 1, desiredObject, types.WithOwner(owner, ownerStrategy))

		// Should return UnsupportedApplyConfigurationError
		require.Error(t, err)

		var unsupportedErr *UnsupportedApplyConfigurationError

		require.ErrorAs(t, err, &unsupportedErr, "expected UnsupportedApplyConfigurationError")
		assert.Contains(t, err.Error(), "does not support ApplyConfiguration: *v1.Secret")
		assert.Nil(t, res)
	})
}

func TestObjectEngine_TeardownWithTeardownWriter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string

		mockSetup func(
			*testutil.CtrlClient,
		)

		expectedResult bool
		expectedError  error
	}{
		{
			name: "deletes with teardown client",
			mockSetup: func(teardownWriter *testutil.CtrlClient) {
				teardownWriter.
					On("Delete", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(nil)
			},
			expectedResult: false,
		},
		{
			name: "returns teardown client errors",
			mockSetup: func(teardownWriter *testutil.CtrlClient) {
				teardownWriter.
					On("Delete", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(errors.New("test error"))
			},
			expectedError:  errors.New("deleting object: test error"),
			expectedResult: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			owner := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					UID:       "12345-678-91011",
					Name:      "some-owner",
					Namespace: "test-namespace",
				},
			}

			obj := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
					"metadata": map[string]interface{}{
						"name":      "test-package",
						"namespace": "test-namespace",
						"annotations": map[string]interface{}{
							"testtest.xxx/revision": "1",
						},
					},
				},
			}
			err := controllerutil.SetControllerReference(owner, obj, scheme.Scheme)
			require.NoError(t, err)

			cache := &cacheMock{}
			engineWriter := testutil.NewClient()   // default engine writer
			teardownWriter := testutil.NewClient() // used during tearDown
			ownerStrategy := ownerhandling.NewNative(scheme.Scheme)
			divergeDetector := &comparatorMock{}

			cache.
				On("Watch", mock.Anything, mock.Anything, mock.Anything).
				Return(nil)

			cache.
				On(
					"Get", mock.Anything,
					client.ObjectKeyFromObject(obj),
					mock.Anything, mock.Anything,
				).
				Run(func(args mock.Arguments) {
					objArg := args.Get(2).(*unstructured.Unstructured)
					*objArg = *obj
				}).
				Return(nil)

			engineWriter.On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

			engineWriter.On("Delete", mock.Anything, mock.Anything, mock.Anything).
				Panic("Delete should not be called on the engine writer when WithTeardownWriter options is used")

			test.mockSetup(teardownWriter)

			oe := NewObjectEngine(
				scheme.Scheme,
				cache, engineWriter,
				divergeDetector,
				testFieldOwner,
				testSystemPrefix,
			)

			result, err := oe.Teardown(
				t.Context(), 1, obj, types.WithOwner(owner, ownerStrategy),
				types.WithTeardownWriter(teardownWriter))
			if test.expectedError != nil {
				assert.EqualError(t, err, test.expectedError.Error())
			} else {
				require.NoError(t, err)
				assert.Equal(t, test.expectedResult, result)
			}
		})
	}
}

func TestObjectEngine_Teardown(t *testing.T) {
	t.Parallel()

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "12345-678",
			Name:      "owner",
			Namespace: "test",
		},
	}

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
	err := controllerutil.SetOwnerReference(owner, obj, scheme.Scheme)
	require.NoError(t, err)

	tests := []struct {
		name     string
		revision int64

		mockSetup func(
			*cacheMock,
			*testutil.CtrlClient,
		)

		expectedResult bool
		expectedError  error
	}{
		{
			name:     "deletes",
			revision: 1,

			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
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

				writer.
					On("Delete", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(nil)
			},
		},
		{
			name:     "revision error",
			revision: 1,

			mockSetup: func(
				cache *cacheMock, _ *testutil.CtrlClient,
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
			},
			expectedResult: true,
		},
		{
			name:     "owner error",
			revision: 1,

			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
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

				writer.On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectedResult: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			cache := &cacheMock{}
			writer := testutil.NewClient()
			ownerStrategy := ownerhandling.NewNative(scheme.Scheme)
			divergeDetector := &comparatorMock{}

			cache.
				On("Watch", mock.Anything, mock.Anything, mock.Anything).
				Return(nil)

			writer.On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

			test.mockSetup(cache, writer)
			oe := NewObjectEngine(
				scheme.Scheme,
				cache, writer,
				divergeDetector,
				testFieldOwner,
				testSystemPrefix,
			)

			deleted, err := oe.Teardown(t.Context(), 1, obj, types.WithOwner(owner, ownerStrategy))
			if test.expectedError != nil {
				assert.EqualError(t, err, test.expectedError.Error())
			} else {
				require.NoError(t, err)
				assert.Equal(t, test.expectedResult, deleted)
			}
		})
	}
}

func TestObjectEngine_Teardown_Orphan(t *testing.T) {
	t.Parallel()

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "12345-678",
			Name:      "owner",
			Namespace: "test",
		},
	}

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

	cache := &cacheMock{}
	writer := testutil.NewClient()
	ownerStrategy := ownerhandling.NewNative(scheme.Scheme)
	divergeDetector := &comparatorMock{}

	cache.
		On("Watch", mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	writer.On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	oe := NewObjectEngine(
		scheme.Scheme,
		cache, writer,
		divergeDetector,
		testFieldOwner,
		testSystemPrefix,
	)
	deleted, err := oe.Teardown(t.Context(), 1, obj, types.WithOrphan(), types.WithOwner(owner, ownerStrategy))
	require.NoError(t, err)

	assert.True(t, deleted)
}

func TestObjectEngine_Teardown_SanityChecks(t *testing.T) {
	t.Parallel()

	oe := &ObjectEngine{}
	owner := &unstructured.Unstructured{}
	desired := &unstructured.Unstructured{}

	t.Run("missing revision", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, "owner revision must be set and start at 1", func() {
			_, _ = oe.Teardown(t.Context(), 0, desired)
		})
	})

	t.Run("missing owner.UID", func(t *testing.T) {
		t.Parallel()
		assert.PanicsWithValue(t, "owner must be persisted to cluster, empty UID", func() {
			_, _ = oe.Teardown(t.Context(), 1, desired, types.WithOwner(owner, nil))
		})
	})
}

type cacheMock struct {
	testutil.CtrlClient
}

type comparatorMock struct {
	mock.Mock
}

func (m *comparatorMock) Compare(
	desiredObject, actualObject Object,
	opts ...types.ComparatorOption,
) (CompareResult, error) {
	args := m.Called(desiredObject, actualObject, opts)

	return args.Get(0).(CompareResult), args.Error(1)
}
