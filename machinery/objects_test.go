package machinery

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"
	"sigs.k8s.io/structured-merge-diff/v6/typed"

	"pkg.package-operator.run/boxcutter/internal/testutil"
	"pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/ownerhandling"
)

const (
	testSystemPrefix = "testtest.xxx"
)

var (
	testOwnerStrategy = ownerhandling.NewNative(scheme.Scheme)
	testOwner         = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "12345-678",
			Name:      "owner",
			Namespace: "test",
		},
	}

	withNativeOwnerMode = nativeOwnerMode("with owner", testOwner)

	withoutOwnerMode = ownerMode{
		name:          "without owner",
		reconcileOpts: func() []types.ObjectReconcileOption { return nil },
		teardownOpts:  func() []types.ObjectTeardownOption { return nil },
		setManaged: func(obj *unstructured.Unstructured) {
			setBoxcutterManagedLabel(obj)
		},
	}
)

// ownerMode captures the per-mode differences for shared reconcile and teardown test scenarios.
type ownerMode struct {
	name          string
	reconcileOpts func() []types.ObjectReconcileOption
	teardownOpts  func() []types.ObjectTeardownOption
	setManaged    func(*unstructured.Unstructured)
}

// nativeOwnerMode creates an ownerMode for the given owner using native owner references.
func nativeOwnerMode(name string, owner client.Object) ownerMode {
	gvks, _, _ := scheme.Scheme.ObjectKinds(owner)
	apiVersion, kind := gvks[0].ToAPIVersionAndKind()

	return ownerMode{
		name: name,
		reconcileOpts: func() []types.ObjectReconcileOption {
			return []types.ObjectReconcileOption{types.WithOwner(owner, testOwnerStrategy)}
		},
		teardownOpts: func() []types.ObjectTeardownOption {
			return []types.ObjectTeardownOption{types.WithOwner(owner, testOwnerStrategy)}
		},
		setManaged: func(obj *unstructured.Unstructured) {
			withOwnerRef(apiVersion, kind, owner.GetName(), string(owner.GetUID()), true)(obj, nil)
			setBoxcutterManagedLabel(obj)
		},
	}
}

// objOption is a functional option for modifying an unstructured test object.
type objOption func(obj *unstructured.Unstructured, r *ownerMode)

type objBuilder func(*ownerMode) *unstructured.Unstructured

// buildObj returns a builder which creates a Secret unstructured object with the given name/namespace and options applied.
func buildObj(name, namespace string, opts ...objOption) objBuilder { //nolint:unparam
	return func(mode *ownerMode) *unstructured.Unstructured {
		obj := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata": map[string]interface{}{
					"name":      name,
					"namespace": namespace,
				},
			},
		}
		for _, opt := range opts {
			opt(obj, mode)
		}

		return obj
	}
}

func withRevision(rev string) objOption {
	return func(obj *unstructured.Unstructured, _ *ownerMode) {
		annotations := obj.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}

		annotations[testSystemPrefix+"/revision"] = rev
		obj.SetAnnotations(annotations)
	}
}

func setBoxcutterManagedLabel(obj *unstructured.Unstructured) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}

	labels[managedByLabel] = managedByLabelValue
	obj.SetLabels(labels)
}

func withManaged(obj *unstructured.Unstructured, r *ownerMode) {
	r.setManaged(obj)
}

func withOwnerRef(apiVersion, kind, name, uid string, controller bool) objOption {
	return func(obj *unstructured.Unstructured, _ *ownerMode) {
		md := obj.Object["metadata"].(map[string]interface{})
		refs, _ := md["ownerReferences"].([]interface{})
		refs = append(refs, map[string]interface{}{
			"apiVersion":         apiVersion,
			"kind":               kind,
			"controller":         controller,
			"name":               name,
			"uid":                uid,
			"blockOwnerDeletion": true,
		})
		md["ownerReferences"] = refs
	}
}

func withBoxcutterManagedLabel(obj *unstructured.Unstructured, _ *ownerMode) {
	setBoxcutterManagedLabel(obj)
}

func withResourceVersion(v string) objOption {
	return func(obj *unstructured.Unstructured, _ *ownerMode) {
		obj.SetResourceVersion(v)
	}
}

//nolint:maintidx
func TestObjectEngine(t *testing.T) {
	t.Parallel()

	defaultModes := []ownerMode{
		withNativeOwnerMode,
		withoutOwnerMode,
	}

	type sharedTestCase struct {
		name     string
		revision int64

		// desiredObject is the bare desired object (no ownerRefs, no labels).
		desiredObject objBuilder

		// actualObject is the on-cluster object
		// nil means NotFound (create path).
		actualObject objBuilder

		// expectedObject is what we expect back
		expectedObject objBuilder

		// opts are additional reconcile options to apply to the test.
		opts []types.ObjectReconcileOption

		// mockSetup configures mocks. actualObject is the mode-transformed actual (nil for create).
		mockSetup func(
			cache *cacheMock,
			writer *testutil.CtrlClient,
			ddm *comparatorMock,
			actualObject *unstructured.Unstructured,
		)

		expectedAction Action

		modes []ownerMode // nil = both default modes

		// expectedError, if set, means Reconcile should return an error containing this substring.
		// expectedAction and expectedObject are ignored when set.
		expectedError string

		// afterAssert, if set, runs additional assertions after the standard checks.
		afterAssert func(t *testing.T, writer *testutil.CtrlClient)
	}

	sharedTests := []sharedTestCase{
		{
			name:           "Created",
			revision:       1,
			desiredObject:  buildObj("testi", "test"),
			actualObject:   nil, // NotFound
			expectedObject: buildObj("testi", "test", withRevision("1"), withManaged),
			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *comparatorMock, _ *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKey{Name: "testi", Namespace: "test"},
						mock.Anything, mock.Anything).
					Return(apierrors.NewNotFound(schema.GroupResource{}, ""))
				ddm.
					On("Compare", mock.Anything, mock.Anything, mock.Anything).
					Return(CompareResult{}, nil)
				writer.
					On("Create", mock.Anything, mock.Anything, mock.Anything).
					Return(nil)
			},
			expectedAction: ActionCreated,
		},
		{ //nolint:dupl
			name:           "Idle",
			revision:       1,
			desiredObject:  buildObj("testi", "test"),
			actualObject:   buildObj("testi", "test", withRevision("1"), withManaged),
			expectedObject: buildObj("testi", "test", withRevision("1"), withManaged),
			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *comparatorMock, actualObject *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything).
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
		},
		{
			name:           "Updated - modified",
			revision:       1,
			desiredObject:  buildObj("testi", "test"),
			actualObject:   buildObj("testi", "test", withRevision("1"), withManaged),
			expectedObject: buildObj("testi", "test", withRevision("1"), withManaged),
			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *comparatorMock, actualObject *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything).
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
		},
		{
			name:           "Recovered",
			revision:       1,
			desiredObject:  buildObj("testi", "test"),
			actualObject:   buildObj("testi", "test", withRevision("1"), withManaged),
			expectedObject: buildObj("testi", "test", withRevision("1"), withManaged),
			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *comparatorMock, actualObject *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*unstructured.Unstructured)
						*obj = *actualObject
					}).
					Return(nil)
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
		},
		{ //nolint:dupl
			name:           "Progressed",
			revision:       1,
			desiredObject:  buildObj("testi", "test"),
			actualObject:   buildObj("testi", "test", withRevision("4"), withManaged),
			expectedObject: buildObj("testi", "test", withRevision("4"), withManaged),
			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *comparatorMock, actualObject *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything).
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
		},
		{
			name:           "Updated noController CollisionProtectionIfNoController",
			revision:       1,
			desiredObject:  buildObj("testi", "test"),
			actualObject:   buildObj("testi", "test"),
			expectedObject: buildObj("testi", "test", withRevision("1"), withManaged),
			opts: []types.ObjectReconcileOption{
				types.WithCollisionProtection(types.CollisionProtectionIfNoController),
			},
			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *comparatorMock, actualObject *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything).
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
		},
		{
			name:           "Collision - no controller",
			revision:       1,
			desiredObject:  buildObj("testi", "test"),
			actualObject:   buildObj("testi", "test"),
			expectedObject: buildObj("testi", "test"),
			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *comparatorMock, actualObject *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything).
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
			expectedAction: ActionCollision,
		},
		{
			name:          "comparison error",
			revision:      1,
			desiredObject: buildObj("testi", "test"),
			actualObject:  buildObj("testi", "test"),
			mockSetup: func(
				cache *cacheMock, _ *testutil.CtrlClient,
				ddm *comparatorMock, actualObject *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*unstructured.Unstructured)
						*obj = *actualObject
					}).
					Return(nil)
				ddm.
					On("Compare", mock.Anything, mock.Anything, mock.Anything).
					Return(CompareResult{}, errors.New("comparison failed"))
			},
			expectedError: "diverge check",
		},
		{
			name:          "Updated takeover from previousOwner",
			revision:      1,
			desiredObject: buildObj("testi", "test"),
			actualObject: buildObj("testi", "test", withRevision("1"),
				withOwnerRef("v1", "ConfigMap", "old-owner", "6789", true)),
			expectedObject: buildObj("testi", "test", withRevision("1"),
				withOwnerRef("v1", "ConfigMap", "old-owner", "6789", false), withManaged),
			modes: []ownerMode{withNativeOwnerMode},
			opts: []types.ObjectReconcileOption{
				types.WithPreviousOwners{&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						UID: "6789", Name: "old-owner", Namespace: "test",
					},
				}},
			},
			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *comparatorMock, actualObject *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything).
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
		},
		{
			name:          "Collision - unknown controller",
			revision:      1,
			desiredObject: buildObj("testi", "test"),
			actualObject: buildObj("testi", "test", withRevision("1"),
				withOwnerRef("v1", "Node", "node1", "xxxx", true)),
			expectedObject: buildObj("testi", "test", withRevision("1"),
				withOwnerRef("v1", "Node", "node1", "xxxx", true)),
			modes: []ownerMode{withNativeOwnerMode},
			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *comparatorMock, actualObject *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything).
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
			expectedAction: ActionCollision,
		},
		{
			name:          "Get error",
			revision:      1,
			desiredObject: buildObj("testi", "test"),
			mockSetup: func(
				cache *cacheMock, _ *testutil.CtrlClient,
				_ *comparatorMock, _ *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKey{Name: "testi", Namespace: "test"},
						mock.Anything, mock.Anything).
					Return(errors.New("cache error"))
			},
			expectedError: "getting object",
		},
		{
			name:          "Updated, CollisionProtectionNone, unknown controller",
			revision:      1,
			desiredObject: buildObj("testi", "test"),
			actualObject: buildObj("testi", "test", withRevision("1"),
				withOwnerRef("v1", "Node", "node1", "xxxx", true)),
			modes: []ownerMode{withNativeOwnerMode},
			opts: []types.ObjectReconcileOption{
				types.WithCollisionProtection(types.CollisionProtectionNone),
			},
			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *comparatorMock, actualObject *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything).
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
		},
		{
			name:          "Collision, CollisionProtectionNone, boxcutter managed",
			revision:      1,
			desiredObject: buildObj("testi", "test"),
			actualObject:  buildObj("testi", "test", withBoxcutterManagedLabel, withRevision("1")),
			modes:         []ownerMode{withNativeOwnerMode},
			opts: []types.ObjectReconcileOption{
				types.WithCollisionProtection(types.CollisionProtectionNone),
			},
			mockSetup: func(
				cache *cacheMock, _ *testutil.CtrlClient,
				ddm *comparatorMock, actualObject *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*unstructured.Unstructured)
						*obj = *actualObject
					}).
					Return(nil)
				ddm.
					On("Compare", mock.Anything, mock.Anything, mock.Anything).
					Return(CompareResult{}, nil)
			},
			expectedAction: ActionCollision,
		},
		{
			name:           "Updated, CollisionProtectionNone",
			revision:       1,
			desiredObject:  buildObj("testi", "test"),
			actualObject:   buildObj("testi", "test", withRevision("1")),
			expectedObject: buildObj("testi", "test", withRevision("1"), withManaged),
			opts: []types.ObjectReconcileOption{
				types.WithCollisionProtection(types.CollisionProtectionNone),
			},
			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				ddm *comparatorMock, actualObject *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything).
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
		},
		{
			name:          "Paused, create skipped",
			revision:      1,
			desiredObject: buildObj("testi", "test"),
			opts: []types.ObjectReconcileOption{
				types.WithPaused{},
			},
			mockSetup: func(
				cache *cacheMock, _ *testutil.CtrlClient,
				_ *comparatorMock, _ *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKey{Name: "testi", Namespace: "test"},
						mock.Anything, mock.Anything).
					Return(apierrors.NewNotFound(schema.GroupResource{}, ""))
			},
			expectedAction: ActionCreated,
			afterAssert: func(t *testing.T, writer *testutil.CtrlClient) {
				t.Helper()
				writer.AssertNotCalled(t, "Create")
			},
		},
		{
			name:          "Paused, apply skipped",
			revision:      1,
			desiredObject: buildObj("testi", "test"),
			actualObject:  buildObj("testi", "test", withRevision("1"), withManaged),
			opts: []types.ObjectReconcileOption{
				types.WithPaused{},
			},
			mockSetup: func(
				cache *cacheMock, _ *testutil.CtrlClient,
				ddm *comparatorMock, actualObject *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*unstructured.Unstructured)
						*obj = *actualObject
					}).
					Return(nil)

				fs := &fieldpath.Set{}
				fs.Insert(fieldpath.MakePathOrDie("spec", "data"))
				ddm.
					On("Compare", mock.Anything, mock.Anything, mock.Anything).
					Return(CompareResult{
						Comparison: &typed.Comparison{
							Added:    &fieldpath.Set{},
							Removed:  &fieldpath.Set{},
							Modified: fs,
						},
					}, nil)
			},
			expectedAction: ActionUpdated,
			afterAssert: func(t *testing.T, writer *testutil.CtrlClient) {
				t.Helper()
				writer.AssertNotCalled(t, "Apply")
			},
		},
		{
			name:          "Paused, recovered returns actual",
			revision:      1,
			desiredObject: buildObj("testi", "test"),
			actualObject: buildObj("testi", "test", withRevision("1"),
				withResourceVersion("999"), withManaged),
			expectedObject: buildObj("testi", "test", withRevision("1"),
				withResourceVersion("999"), withManaged),
			opts: []types.ObjectReconcileOption{
				types.WithPaused{},
			},
			mockSetup: func(
				cache *cacheMock, _ *testutil.CtrlClient,
				ddm *comparatorMock, actualObject *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*unstructured.Unstructured)
						*obj = *actualObject
					}).
					Return(nil)
				ddm.
					On("Compare", mock.Anything, mock.Anything, mock.Anything).
					Return(CompareResult{
						ConflictingMangers: []CompareResultManagedFields{
							{Manager: "some-other-manager"},
						},
					}, nil)
			},
			expectedAction: ActionRecovered,
			afterAssert: func(t *testing.T, writer *testutil.CtrlClient) {
				t.Helper()
				writer.AssertNotCalled(t, "Apply")
			},
		},
		{
			name:          "Paused, takeover returns actual",
			revision:      1,
			desiredObject: buildObj("testi", "test"),
			actualObject: buildObj("testi", "test", withRevision("1"),
				withResourceVersion("888"),
				withOwnerRef("v1", "ConfigMap", "old-owner", "6789", true)),
			expectedObject: buildObj("testi", "test", withRevision("1"),
				withResourceVersion("888"),
				withOwnerRef("v1", "ConfigMap", "old-owner", "6789", true)),
			modes: []ownerMode{withNativeOwnerMode},
			opts: []types.ObjectReconcileOption{
				types.WithPaused{},
				types.WithPreviousOwners{&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						UID: "6789", Name: "old-owner", Namespace: "test",
					},
				}},
			},
			mockSetup: func(
				cache *cacheMock, _ *testutil.CtrlClient,
				ddm *comparatorMock, actualObject *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*unstructured.Unstructured)
						*obj = *actualObject
					}).
					Return(nil)
				ddm.
					On("Compare", mock.Anything, mock.Anything, mock.Anything).
					Return(CompareResult{}, nil)
			},
			expectedAction: ActionUpdated,
			afterAssert: func(t *testing.T, writer *testutil.CtrlClient) {
				t.Helper()
				writer.AssertNotCalled(t, "Apply")
			},
		},
	}

	for _, tc := range sharedTests {
		modes := tc.modes
		if len(modes) == 0 {
			modes = defaultModes
		}

		for _, mode := range modes {
			t.Run(tc.name+"/"+mode.name, func(t *testing.T) {
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

				desiredObject := tc.desiredObject(&mode)

				var actualObject *unstructured.Unstructured
				if tc.actualObject != nil {
					actualObject = tc.actualObject(&mode)
				}

				tc.mockSetup(cache, writer, divergeDetector, actualObject)

				res, err := oe.Reconcile(
					t.Context(), tc.revision, desiredObject,
					slices.Concat(mode.reconcileOpts(), tc.opts)...,
				)

				if tc.expectedObject != nil {
					expectedObject := tc.expectedObject(&mode)
					assert.Equal(t, expectedObject, res.Object())
				}

				if tc.expectedError == "" {
					require.NoError(t, err)
					assert.Equal(t, tc.expectedAction, res.Action())
				} else {
					require.Error(t, err)
					assert.Contains(t, err.Error(), tc.expectedError)
					assert.Nil(t, res)
				}

				if tc.afterAssert != nil {
					tc.afterAssert(t, writer)
				}
			})
		}
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

//nolint:maintidx
func TestObjectEngine_Teardown(t *testing.T) {
	t.Parallel()

	defaultModes := []ownerMode{
		withNativeOwnerMode,
		withoutOwnerMode,
	}

	sharedTests := []struct {
		name          string
		revision      int64
		desiredObject objBuilder
		actualObject  objBuilder // nil means object does not exist on cluster
		opts          func() []types.ObjectTeardownOption
		mockSetup     func(
			cache *cacheMock,
			writer *testutil.CtrlClient,
			actualObject *unstructured.Unstructured,
		)
		modes         []ownerMode // nil = both default modes
		expectedGone  bool
		expectedError string
	}{
		{
			name:          "Orphan",
			revision:      1,
			desiredObject: buildObj("testi", "test"),
			actualObject:  buildObj("testi", "test", withRevision("1"), withManaged),
			opts: func() []types.ObjectTeardownOption {
				return []types.ObjectTeardownOption{types.WithOrphan()}
			},
			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				actualObject *unstructured.Unstructured,
			) {
				writer.On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
				cache.
					On("Get", mock.Anything,
						client.ObjectKey{Name: "testi", Namespace: "test"},
						mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*unstructured.Unstructured)
						*obj = *actualObject
					}).
					Return(nil)
			},
			expectedGone: true,
		},
		{
			name:          "NotFound on Get",
			revision:      1,
			desiredObject: buildObj("testi", "test"),
			mockSetup: func(
				cache *cacheMock, _ *testutil.CtrlClient,
				_ *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKey{Name: "testi", Namespace: "test"},
						mock.Anything, mock.Anything).
					Return(apierrors.NewNotFound(schema.GroupResource{}, ""))
			},
			expectedGone: true,
		},
		{
			name:          "NoMatchError on Get",
			revision:      1,
			desiredObject: buildObj("testi", "test"),
			mockSetup: func(
				cache *cacheMock, _ *testutil.CtrlClient,
				_ *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKey{Name: "testi", Namespace: "test"},
						mock.Anything, mock.Anything).
					Return(&meta.NoResourceMatchError{
						PartialResource: schema.GroupVersionResource{
							Group: "", Version: "v1", Resource: "secrets",
						},
					})
			},
			expectedGone: true,
		},
		{
			name:          "Get error",
			revision:      1,
			desiredObject: buildObj("testi", "test"),
			mockSetup: func(
				cache *cacheMock, _ *testutil.CtrlClient,
				_ *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKey{Name: "testi", Namespace: "test"},
						mock.Anything, mock.Anything).
					Return(errors.New("cache error"))
			},
			expectedError: "getting object before deletion",
		},
		{
			name:          "Deletes",
			revision:      1,
			desiredObject: buildObj("testi", "test"),
			actualObject:  buildObj("testi", "test", withRevision("1"), withManaged),
			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				actualObject *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*unstructured.Unstructured)
						*obj = *actualObject
					}).
					Return(nil)
				writer.
					On("Delete", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(nil)
			},
			expectedGone: false,
		},
		{
			name:          "Delete NotFound",
			revision:      1,
			desiredObject: buildObj("testi", "test"),
			actualObject:  buildObj("testi", "test", withRevision("1"), withManaged),
			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				actualObject *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*unstructured.Unstructured)
						*obj = *actualObject
					}).
					Return(nil)
				writer.
					On("Delete", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(apierrors.NewNotFound(schema.GroupResource{}, ""))
			},
			expectedGone: true,
		},
		{
			name:          "Delete error",
			revision:      1,
			desiredObject: buildObj("testi", "test"),
			actualObject:  buildObj("testi", "test", withRevision("1"), withManaged),
			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				actualObject *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*unstructured.Unstructured)
						*obj = *actualObject
					}).
					Return(nil)
				writer.
					On("Delete", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(errors.New("delete failed"))
			},
			expectedError: "deleting object",
		},
		{
			name:          "TeardownWriter deletes",
			revision:      1,
			desiredObject: buildObj("testi", "test"),
			actualObject:  buildObj("testi", "test", withRevision("1"), withManaged),
			opts: func() []types.ObjectTeardownOption {
				teardownWriter := testutil.NewClient()
				teardownWriter.
					On("Delete", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(nil)

				return []types.ObjectTeardownOption{types.WithTeardownWriter(teardownWriter)}
			},
			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				actualObject *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*unstructured.Unstructured)
						*obj = *actualObject
					}).
					Return(nil)
				writer.On("Delete", mock.Anything, mock.Anything, mock.Anything).
					Panic("Delete should not be called on the engine writer")
			},
			expectedGone: false,
		},
		{
			name:          "TeardownWriter error",
			revision:      1,
			desiredObject: buildObj("testi", "test"),
			actualObject:  buildObj("testi", "test", withRevision("1"), withManaged),
			opts: func() []types.ObjectTeardownOption {
				teardownWriter := testutil.NewClient()
				teardownWriter.
					On("Delete", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(errors.New("teardown delete failed"))

				return []types.ObjectTeardownOption{types.WithTeardownWriter(teardownWriter)}
			},
			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				actualObject *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*unstructured.Unstructured)
						*obj = *actualObject
					}).
					Return(nil)
				writer.On("Delete", mock.Anything, mock.Anything, mock.Anything).
					Panic("Delete should not be called on the engine writer")
			},
			expectedError: "deleting object: teardown delete failed",
		},
		{
			name:          "Revision mismatch",
			revision:      1,
			desiredObject: buildObj("testi", "test"),
			actualObject:  buildObj("testi", "test", withRevision("4")),
			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				actualObject *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*unstructured.Unstructured)
						*obj = *actualObject
					}).
					Return(nil)
				// Patch may be called in with-owner mode to remove owner ref.
				writer.On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectedGone: true,
		},
		{
			name:          "Revision mismatch, unknown controller",
			revision:      1,
			desiredObject: buildObj("testi", "test"),
			actualObject: buildObj("testi", "test", withRevision("4"),
				withOwnerRef("v1", "ConfigMap", "other-owner", "12345-678", true)),
			modes: []ownerMode{withNativeOwnerMode},
			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				actualObject *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*unstructured.Unstructured)
						*obj = *actualObject
					}).
					Return(nil)
				writer.On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectedGone: true,
		},
		{
			name:          "Revision mismatch, no controller",
			revision:      1,
			desiredObject: buildObj("testi", "test"),
			actualObject:  buildObj("testi", "test", withRevision("2")),
			mockSetup: func(
				cache *cacheMock, writer *testutil.CtrlClient,
				actualObject *unstructured.Unstructured,
			) {
				cache.
					On("Get", mock.Anything,
						client.ObjectKeyFromObject(actualObject),
						mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						obj := args.Get(2).(*unstructured.Unstructured)
						*obj = *actualObject
					}).
					Return(nil)
				writer.On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectedGone: true,
		},
		{
			name:          "OrphanFinalizer",
			revision:      1,
			desiredObject: buildObj("testi", "test"),
			actualObject:  buildObj("testi", "test", withManaged),
			modes: []ownerMode{nativeOwnerMode("with orphan finalizer owner", &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					UID: "12345-678", Name: "owner", Namespace: "test",
					Finalizers: []string{"orphan"},
				},
			})},
			mockSetup: func(
				_ *cacheMock, writer *testutil.CtrlClient,
				_ *unstructured.Unstructured,
			) {
				writer.On("Patch", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
			},
			expectedGone: true,
		},
	}

	for _, tc := range sharedTests {
		modes := tc.modes
		if len(modes) == 0 {
			modes = defaultModes
		}

		for _, mode := range modes {
			t.Run(tc.name+"/"+mode.name, func(t *testing.T) {
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

				desiredObject := tc.desiredObject(&mode)

				var actualObject *unstructured.Unstructured
				if tc.actualObject != nil {
					actualObject = tc.actualObject(&mode)
				}

				tc.mockSetup(cache, writer, actualObject)

				opts := mode.teardownOpts()
				if tc.opts != nil {
					opts = append(opts, tc.opts()...)
				}

				gone, err := oe.Teardown(
					t.Context(), tc.revision, desiredObject,
					opts...,
				)

				if tc.expectedError == "" {
					require.NoError(t, err)
					assert.Equal(t, tc.expectedGone, gone)
				} else {
					require.Error(t, err)
					assert.Contains(t, err.Error(), tc.expectedError)
				}
			})
		}
	}
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

func TestObjectEngine_IsBoxcutterManaged_FalseCase(t *testing.T) {
	t.Parallel()

	engine := NewObjectEngine(
		scheme.Scheme,
		testutil.NewClient(),
		testutil.NewClient(),
		&comparatorMock{},
		"test-owner",
		"test-prefix",
	)

	tests := []struct {
		name   string
		labels map[string]string
	}{
		{
			name:   "no labels",
			labels: nil,
		},
		{
			name:   "empty labels",
			labels: map[string]string{},
		},
		{
			name: "other labels only",
			labels: map[string]string{
				"app": "myapp",
				"env": "prod",
			},
		},
		{
			name: "contains but doesn't start with prefix",
			labels: map[string]string{
				"my-test-prefix-managed": "true",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			obj := &unstructured.Unstructured{}
			obj.SetLabels(tt.labels)

			assert.False(t, engine.isBoxcutterManaged(obj))
		})
	}
}

func TestObjectEngine_GetObjectRevision_Error(t *testing.T) {
	t.Parallel()

	oe := NewObjectEngine(
		scheme.Scheme,
		testutil.NewClient(),
		testutil.NewClient(),
		&comparatorMock{},
		testFieldOwner,
		testSystemPrefix,
	)

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "test",
				"namespace": "test",
				"annotations": map[string]interface{}{
					testSystemPrefix + "/revision": "not-a-number",
				},
			},
		},
	}

	_, err := oe.getObjectRevision(obj)
	require.Error(t, err)
}

func TestObjectEngine_MigrateFieldManagersToSSA_NoPatch(t *testing.T) {
	t.Parallel()

	writer := testutil.NewClient()
	oe := NewObjectEngine(
		scheme.Scheme,
		testutil.NewClient(),
		writer,
		&comparatorMock{},
		testFieldOwner,
		testSystemPrefix,
	)

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "test",
				"namespace": "test",
			},
		},
	}

	err := oe.migrateFieldManagersToSSA(context.Background(), obj)
	require.NoError(t, err)

	writer.AssertNotCalled(t, "Patch")
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
