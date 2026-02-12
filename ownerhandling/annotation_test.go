package ownerhandling

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	bctypes "pkg.package-operator.run/boxcutter/machinery/types"
)

const testAnnotationKey = "xyz/owner"

func TestAnnotationRevisionMetadata_RemoveFrom(t *testing.T) {
	t.Parallel()

	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cm1",
			Namespace: "test",
			UID:       types.UID("1234"),
			Annotations: map[string]string{
				testAnnotationKey: `[{"uid":"123456", "kind":"ConfigMap", "name":"cm1", "apiVersion":"v1"}]`,
			},
		},
	}
	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cm1",
			Namespace: obj.Namespace,
			UID:       types.UID("123456"),
		},
	}

	h := NewAnnotationStrategy(testAnnotationKey)
	m := h.NewRevisionMetadata(owner, testScheme)
	m.RemoveFrom(obj)

	assert.Equal(t, `[]`, obj.Annotations[testAnnotationKey])
}

func TestAnnotationRevisionMetadata_SetCurrent(t *testing.T) {
	t.Parallel()

	cm1 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cm1",
			Namespace: "cmtestns",
			UID:       types.UID("1234"),
		},
	}
	obj := &corev1.Secret{}

	h := NewAnnotationStrategy(testAnnotationKey)
	m := h.NewRevisionMetadata(cm1, testScheme)
	err := m.SetCurrent(obj)
	require.NoError(t, err)

	// Verify the annotation was set
	assert.NotEmpty(t, obj.Annotations[testAnnotationKey])
	assert.True(t, m.IsCurrent(obj))

	// Setting a second controller should fail
	cm2 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cm2",
			Namespace: "cmtestns",
			UID:       types.UID("56789"),
		},
	}

	m2 := h.NewRevisionMetadata(cm2, testScheme)
	err = m2.SetCurrent(obj)
	require.Error(t, err)

	var alreadyOwnedErr *controllerutil.AlreadyOwnedError

	require.ErrorAs(t, err, &alreadyOwnedErr)

	// Verify cm1 is still the controller
	require.True(t, m.IsCurrent(obj))
	require.False(t, m2.IsCurrent(obj))

	// We should be able to set a new controller using WithAllowUpdate
	err = m2.SetCurrent(obj, bctypes.WithAllowUpdate)
	require.NoError(t, err)
	require.True(t, m2.IsCurrent(obj))
	require.False(t, m.IsCurrent(obj))
}

func TestAnnotationEnqueueOwnerHandler_GetOwnerReconcileRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		isOwnerController *bool
		ownerType         client.Object
		isController      bool
		requestExpected   bool
	}{
		{
			name:              "owner is controller, enqueue is controller, types match",
			isOwnerController: ptr.To(true),
			ownerType:         &corev1.ConfigMap{},
			isController:      true,
			requestExpected:   true,
		},
		{
			name:              "owner is not controller, enqueue is controller, types match",
			isOwnerController: ptr.To(false),
			ownerType:         &corev1.ConfigMap{},
			isController:      true,
			requestExpected:   false,
		},
		{
			name:              "owner is controller, enqueue is not controller, types match",
			isOwnerController: ptr.To(true),
			ownerType:         &corev1.ConfigMap{},
			isController:      false,
			requestExpected:   true,
		},
		{
			name:              "owner is not controller, enqueue is not controller, types match",
			isOwnerController: ptr.To(false),
			ownerType:         &corev1.ConfigMap{},
			isController:      false,
			requestExpected:   true,
		},
		{
			name:              "owner controller is nil, enqueue is controller, types match",
			isOwnerController: nil,
			ownerType:         &corev1.ConfigMap{},
			isController:      true,
			requestExpected:   false,
		},
		{
			name:              "owner controller is nil, enqueue is not controller, types match",
			isOwnerController: nil,
			ownerType:         &corev1.ConfigMap{},
			isController:      false,
			requestExpected:   true,
		},
		{
			name:              "owner is controller, enqueue is controller, types do not match",
			isOwnerController: ptr.To(true),
			ownerType:         &appsv1.Deployment{},
			isController:      true,
			requestExpected:   false,
		},
	}

	owner := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("cm1___3245"),
			Name:      "cm1",
			Namespace: "cm1namespace",
		},
	}

	newOwner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			UID:       types.UID("cm2___3245"),
			Name:      "cm2",
			Namespace: "cm2namespace",
		},
	}

	h := NewAnnotationStrategy(testAnnotationKey)
	revisionMetadata := h.NewRevisionMetadata(owner, testScheme)
	newRevisionMetadata := h.NewRevisionMetadata(newOwner, testScheme)

	for i := range tests {
		test := tests[i]

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			obj := &corev1.Secret{}
			err := revisionMetadata.SetCurrent(obj)
			require.NoError(t, err)

			if test.isOwnerController == nil || !*test.isOwnerController {
				// New revision will take over ownership, so 'owner' is no longer the controller
				err = newRevisionMetadata.SetCurrent(obj, bctypes.WithAllowUpdate)
				require.NoError(t, err)

				newRevisionMetadata.RemoveFrom(obj)
			}

			enqueue := h.EnqueueRequestForOwner(testScheme, test.ownerType, test.isController).(*annotationEnqueueRequestForOwner)
			req := enqueue.getOwnerReconcileRequest(obj)

			if test.requestExpected {
				assert.Equal(t, []reconcile.Request{
					{
						NamespacedName: client.ObjectKey{
							Name:      owner.Name,
							Namespace: owner.Namespace,
						},
					},
				}, req)
			} else {
				assert.Empty(t, req)
			}
		})
	}
}

func TestAnnotationEnqueueOwnerHandler_ParseOwnerTypeGroupKind(t *testing.T) {
	t.Parallel()

	h := &annotationEnqueueRequestForOwner{
		ownerType:    &appsv1.Deployment{},
		isController: true,
	}

	scheme := runtime.NewScheme()
	require.NoError(t, appsv1.AddToScheme(scheme))
	err := h.parseOwnerTypeGroupKind(scheme)
	require.NoError(t, err)

	expectedGK := schema.GroupKind{
		Group: "apps",
		Kind:  "Deployment",
	}
	assert.Equal(t, expectedGK, h.ownerGK)
}

func TestAnnotationRevisionMetadata_IsCurrent(t *testing.T) {
	t.Parallel()

	obj := &corev1.Secret{}
	cm1 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cm1",
			Namespace: obj.Namespace,
			UID:       types.UID("1234"),
		},
	}

	h := NewAnnotationStrategy(testAnnotationKey)
	m1 := h.NewRevisionMetadata(cm1, testScheme)
	err := m1.SetCurrent(obj)
	require.NoError(t, err)

	cm2 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cm2",
			Namespace: obj.Namespace,
			UID:       types.UID("56789"),
		},
	}
	m2 := h.NewRevisionMetadata(cm2, testScheme)

	assert.True(t, m1.IsCurrent(obj))
	assert.False(t, m2.IsCurrent(obj))
}

func TestAnnotationRevisionMetadata_GetCurrent(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name               string
		annotation         string
		expectedController metav1.OwnerReference
		expectedFound      bool
	}{
		{
			name:               "no owner references",
			annotation:         "",
			expectedController: metav1.OwnerReference{},
			expectedFound:      false,
		},
		{
			name:               "empty owner references array",
			annotation:         "[]",
			expectedController: metav1.OwnerReference{},
			expectedFound:      false,
		},
		{
			name:               "no controller owner reference",
			annotation:         `[{"apiVersion":"v1","kind":"ConfigMap","name":"cm1","namespace":"test","uid":"1234"},{"apiVersion":"v1","kind":"Secret","name":"secret1","namespace":"test","uid":"5678","controller":false}]`,
			expectedController: metav1.OwnerReference{},
			expectedFound:      false,
		},
		{
			name:       "has controller owner reference",
			annotation: `[{"apiVersion":"v1","kind":"ConfigMap","name":"cm1","namespace":"test","uid":"1234","controller":false},{"apiVersion":"v1","kind":"Secret","name":"secret1","namespace":"test","uid":"5678","controller":true}]`,
			expectedController: metav1.OwnerReference{
				APIVersion: "v1",
				Kind:       "Secret",
				Name:       "secret1",
				UID:        types.UID("5678"),
				Controller: ptr.To(true),
			},
			expectedFound: true,
		},
	}

	// Create a dummy owner for creating the metadata (required for constructor)
	dummyOwner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "dummy",
			UID:  types.UID("dummy-uid"),
		},
	}

	for i := range testCases {
		tc := testCases[i]
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := NewAnnotationStrategy(testAnnotationKey)
			m := h.NewRevisionMetadata(dummyOwner, testScheme)

			obj := &corev1.Secret{}
			if tc.annotation != "" {
				obj.Annotations = map[string]string{testAnnotationKey: tc.annotation}
			}

			controller := m.GetCurrent(obj)
			if tc.expectedFound {
				require.NotNil(t, controller)
				// Type assert to get the OwnerReference
				ownerRef, ok := controller.(*metav1.OwnerReference)
				require.True(t, ok)
				assert.Equal(t, tc.expectedController, *ownerRef)
			} else {
				assert.Nil(t, controller)
			}
		})
	}
}

func TestAnnotationRevisionMetadata_CopyReferences(t *testing.T) {
	t.Parallel()

	dummyOwner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "dummy",
			UID:  types.UID("dummy-uid"),
		},
	}
	h := NewAnnotationStrategy(testAnnotationKey)
	m := h.NewRevisionMetadata(dummyOwner, testScheme)

	objA := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret-a",
			Namespace: "test",
			Annotations: map[string]string{
				testAnnotationKey: `[{"apiVersion":"v1","kind":"Pod","name":"pod1","namespace":"test","uid":"1234","controller":true},{"apiVersion":"apps/v1","kind":"Deployment","name":"deploy1","namespace":"test","uid":"5678","controller":false}]`,
			},
		},
	}
	objB := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "configmap-b",
			Namespace: "test",
		},
	}

	m.CopyReferences(objA, objB)

	// Verify objB has annotations set
	expectedOwnerRefs := []annotationOwnerRef{
		{
			OwnerReference: metav1.OwnerReference{
				APIVersion: "v1",
				Kind:       "Pod",
				Name:       "pod1",
				UID:        types.UID("1234"),
				// Controller is released (set to nil)
			},
			Namespace: "test",
		},
		{
			OwnerReference: metav1.OwnerReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "deploy1",
				UID:        types.UID("5678"),
				// Controller is released (set to nil)
			},
			Namespace: "test",
		},
	}

	require.NotEmpty(t, objB.Annotations[testAnnotationKey])
	ownerRefs := m.getOwnerReferences(objB)
	require.ElementsMatch(t, expectedOwnerRefs, ownerRefs)

	// Verify that controller is released (set to nil)
	// GetCurrent should return nil since all controllers are released
	controller := m.GetCurrent(objB)
	assert.Nil(t, controller, "CopyReferences should release all controllers")
}

func TestAnnotationRevisionMetadata_GetOwnerReferencesEdgeCases(t *testing.T) {
	t.Parallel()

	dummyOwner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "dummy",
			UID:  types.UID("dummy-uid"),
		},
	}

	testCases := []struct {
		name           string
		obj            metav1.Object
		expectedNilRef bool
	}{
		{
			name:           "no annotations",
			obj:            &corev1.Secret{},
			expectedNilRef: true,
		},
		{
			name: "empty annotation key",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						testAnnotationKey: "",
					},
				},
			},
			expectedNilRef: true,
		},
		{
			name: "empty owner references array",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						testAnnotationKey: "[]",
					},
				},
			},
			expectedNilRef: true,
		},
	}

	for i := range testCases {
		tc := testCases[i]
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h := NewAnnotationStrategy(testAnnotationKey)
			m := h.NewRevisionMetadata(dummyOwner, testScheme)

			controller := m.GetCurrent(tc.obj)
			if tc.expectedNilRef {
				assert.Nil(t, controller)
			}
		})
	}
}

func TestAnnotationRevisionMetadata_ReferSameObjectEdgeCases(t *testing.T) {
	t.Parallel()

	// Test via IsCurrent with various owner reference configurations.
	// Note: The owner is always a ConfigMap, so its GVK is always v1/ConfigMap (core group).
	// We test referSameObject by varying the annotation ref's GVK fields.
	testCases := []struct {
		name          string
		ownerName     string
		ownerUID      types.UID
		refAnnotation string
		expectedMatch bool
	}{
		{
			name:          "same objects",
			ownerName:     "cm1",
			ownerUID:      types.UID("1234"),
			refAnnotation: `[{"apiVersion":"v1","kind":"ConfigMap","name":"cm1","uid":"1234","controller":true}]`,
			expectedMatch: true,
		},
		{
			name:          "different groups - core vs apps",
			ownerName:     "cm1",
			ownerUID:      types.UID("1234"),
			refAnnotation: `[{"apiVersion":"apps/v1","kind":"ConfigMap","name":"cm1","uid":"1234","controller":true}]`,
			expectedMatch: false, // Owner is core group (v1), ref is apps group
		},
		{
			name:          "same group different versions - both core",
			ownerName:     "cm1",
			ownerUID:      types.UID("1234"),
			refAnnotation: `[{"apiVersion":"v1beta1","kind":"ConfigMap","name":"cm1","uid":"1234","controller":true}]`,
			expectedMatch: true, // Same group (core), different versions should match
		},
		{
			name:          "different kinds",
			ownerName:     "cm1",
			ownerUID:      types.UID("1234"),
			refAnnotation: `[{"apiVersion":"v1","kind":"Secret","name":"cm1","uid":"1234","controller":true}]`,
			expectedMatch: false,
		},
		{
			name:          "different names",
			ownerName:     "cm1",
			ownerUID:      types.UID("1234"),
			refAnnotation: `[{"apiVersion":"v1","kind":"ConfigMap","name":"cm2","uid":"1234","controller":true}]`,
			expectedMatch: false,
		},
		{
			name:          "different UIDs",
			ownerName:     "cm1",
			ownerUID:      types.UID("1234"),
			refAnnotation: `[{"apiVersion":"v1","kind":"ConfigMap","name":"cm1","uid":"5678","controller":true}]`,
			expectedMatch: false,
		},
	}

	for i := range testCases {
		tc := testCases[i]
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			owner := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: tc.ownerName,
					UID:  tc.ownerUID,
				},
			}
			obj := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						testAnnotationKey: tc.refAnnotation,
					},
				},
			}

			h := NewAnnotationStrategy(testAnnotationKey)
			m := h.NewRevisionMetadata(owner, testScheme)
			result := m.IsCurrent(obj)
			assert.Equal(t, tc.expectedMatch, result)
		})
	}
}

func TestAnnotationRevisionMetadata_RemoveFromNotFound(t *testing.T) {
	t.Parallel()

	obj := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				testAnnotationKey: `[{"uid":"1234", "kind":"ConfigMap", "name":"cm1", "apiVersion":"v1"}]`,
			},
		},
	}
	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cm2",
			UID:  types.UID("5678"),
		},
	}

	h := NewAnnotationStrategy(testAnnotationKey)
	m := h.NewRevisionMetadata(owner, testScheme)
	initialAnnotation := obj.Annotations[testAnnotationKey]
	m.RemoveFrom(obj)
	finalAnnotation := obj.Annotations[testAnnotationKey]

	assert.Equal(t, initialAnnotation, finalAnnotation)
}

// Tests for annotationOwnerRef struct methods

func TestAnnotationOwnerRef_IsController(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name               string
		annOwnerRef        annotationOwnerRef
		expectedController bool
	}{
		{
			name:               "annotation owner ref not defined",
			annOwnerRef:        annotationOwnerRef{},
			expectedController: false,
		},
		{
			name: "controller is null",
			annOwnerRef: annotationOwnerRef{
				OwnerReference: metav1.OwnerReference{
					Controller: nil,
				},
			},
			expectedController: false,
		},
		{
			name: "controller is false",
			annOwnerRef: annotationOwnerRef{
				OwnerReference: metav1.OwnerReference{
					Controller: ptr.To(false),
				},
			},
			expectedController: false,
		},
		{
			name: "controller is defined and true",
			annOwnerRef: annotationOwnerRef{
				OwnerReference: metav1.OwnerReference{
					Controller: ptr.To(true),
				},
			},
			expectedController: true,
		},
	}

	for i := range testCases {
		tc := testCases[i]
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			annOwnerRef := tc.annOwnerRef
			resultController := annOwnerRef.isController()
			assert.Equal(t, tc.expectedController, resultController)
		})
	}
}

func TestAnnotationRevisionMetadata_IsNamespaceAllowed(t *testing.T) {
	t.Parallel()

	// For annotation-based ownership, cross-namespace is always allowed
	testCases := []struct {
		name           string
		ownerNamespace string
		objNamespace   string
	}{
		{
			name:           "same namespace",
			ownerNamespace: "test",
			objNamespace:   "test",
		},
		{
			name:           "different namespace",
			ownerNamespace: "test",
			objNamespace:   "other",
		},
		{
			name:           "cluster-scoped owner",
			ownerNamespace: "",
			objNamespace:   "any-namespace",
		},
	}

	for i := range testCases {
		tc := testCases[i]
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			owner := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "owner",
					Namespace: tc.ownerNamespace,
					UID:       types.UID("owner-uid"),
				},
			}
			obj := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "obj",
					Namespace: tc.objNamespace,
				},
			}

			h := NewAnnotationStrategy(testAnnotationKey)
			m := h.NewRevisionMetadata(owner, testScheme)
			// Annotation-based ownership always allows cross-namespace
			assert.True(t, m.IsNamespaceAllowed(obj))
		})
	}
}

func TestAnnotationRevisionMetadata_PanicsOnEmptyUID(t *testing.T) {
	t.Parallel()

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "owner-without-uid",
			Namespace: "test",
			// UID is empty - not persisted to cluster
		},
	}

	assert.Panics(t, func() {
		h := NewAnnotationStrategy(testAnnotationKey)
		_ = h.NewRevisionMetadata(owner, testScheme)
	})
}
