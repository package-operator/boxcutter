package ownerhandling

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	bctypes "pkg.package-operator.run/boxcutter/machinery/types"
)

var testScheme = scheme.Scheme

func TestNativeRevisionMetadata_RemoveFrom(t *testing.T) {
	t.Parallel()

	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cm1",
			Namespace: "test",
			UID:       types.UID("1234"),
			OwnerReferences: []metav1.OwnerReference{
				{Name: "cm1", UID: types.UID("123456"), Kind: "ConfigMap", APIVersion: "v1"},
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

	m := NewNativeRevisionMetadata(owner, testScheme)
	m.RemoveFrom(obj)

	assert.Equal(t, []metav1.OwnerReference{}, obj.GetOwnerReferences())
}

func TestNativeRevisionMetadata_SetCurrent(t *testing.T) {
	t.Parallel()

	cm1 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cm1",
			Namespace: "test",
			UID:       types.UID("1234"),
		},
	}
	obj := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
		},
	}

	m := NewNativeRevisionMetadata(cm1, testScheme)
	err := m.SetCurrent(obj)
	require.NoError(t, err)
	require.True(t, m.IsCurrent(obj))

	toOwnerRef := func(obj metav1.Object) metav1.OwnerReference {
		return metav1.OwnerReference{
			APIVersion:         "v1",
			Kind:               "ConfigMap",
			Name:               obj.GetName(),
			UID:                obj.GetUID(),
			BlockOwnerDeletion: ptr.To(true),
			Controller:         ptr.To(true),
		}
	}

	cm1Ref := toOwnerRef(cm1)

	ownerRefs := obj.GetOwnerReferences()
	require.ElementsMatch(t, ownerRefs, []metav1.OwnerReference{cm1Ref})

	// Setting a second controller should fail
	cm2 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cm2",
			Namespace: "test",
			UID:       types.UID("56789"),
		},
	}
	cm2Ref := toOwnerRef(cm2)

	m2 := NewNativeRevisionMetadata(cm2, testScheme)
	err = m2.SetCurrent(obj)
	require.Error(t, err)

	var alreadyOwnedErr *controllerutil.AlreadyOwnedError

	require.ErrorAs(t, err, &alreadyOwnedErr)
	require.True(t, m.IsCurrent(obj))
	require.False(t, m2.IsCurrent(obj))

	// We should be able to set a new controller using WithAllowUpdate
	err = m2.SetCurrent(obj, bctypes.WithAllowUpdate)
	require.NoError(t, err)
	require.True(t, m2.IsCurrent(obj))
	require.False(t, m.IsCurrent(obj))

	ownerRefs = obj.GetOwnerReferences()
	cm1Ref.Controller = nil
	require.ElementsMatch(t, ownerRefs, []metav1.OwnerReference{cm1Ref, cm2Ref})
}

func TestNativeRevisionMetadata_IsCurrent(t *testing.T) {
	t.Parallel()

	obj := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test",
		},
	}
	cm1 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cm1",
			Namespace: obj.Namespace,
			UID:       types.UID("1234"),
		},
	}

	m1 := NewNativeRevisionMetadata(cm1, testScheme)
	err := m1.SetCurrent(obj)
	require.NoError(t, err)

	cm2 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cm2",
			Namespace: obj.Namespace,
			UID:       types.UID("56789"),
		},
	}
	m2 := NewNativeRevisionMetadata(cm2, testScheme)

	assert.True(t, m1.IsCurrent(obj))
	assert.False(t, m2.IsCurrent(obj))
}

func TestNativeRevisionMetadata_GetCurrent(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name               string
		ownerRefs          []metav1.OwnerReference
		expectedController metav1.OwnerReference
		expectedFound      bool
	}{
		{
			name:               "no owner references",
			ownerRefs:          []metav1.OwnerReference{},
			expectedController: metav1.OwnerReference{},
			expectedFound:      false,
		},
		{
			name: "no controller owner reference",
			ownerRefs: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Name:       "cm1",
					UID:        types.UID("1234"),
					Controller: nil,
				},
				{
					APIVersion: "v1",
					Kind:       "Secret",
					Name:       "secret1",
					UID:        types.UID("5678"),
					Controller: ptr.To(false),
				},
			},
			expectedController: metav1.OwnerReference{},
			expectedFound:      false,
		},
		{
			name: "has controller owner reference",
			ownerRefs: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Name:       "cm1",
					UID:        types.UID("1234"),
					Controller: ptr.To(false),
				},
				{
					APIVersion: "v1",
					Kind:       "Secret",
					Name:       "secret1",
					UID:        types.UID("5678"),
					Controller: ptr.To(true),
				},
			},
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

			m := NewNativeRevisionMetadata(dummyOwner, testScheme)
			obj := &corev1.Secret{}
			obj.SetOwnerReferences(tc.ownerRefs)

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

func TestNativeRevisionMetadata_CopyReferences(t *testing.T) {
	t.Parallel()

	dummyOwner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "dummy",
			UID:  types.UID("dummy-uid"),
		},
	}
	m := NewNativeRevisionMetadata(dummyOwner, testScheme)

	objA := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret-a",
			Namespace: "test",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       "pod1",
					UID:        types.UID("1234"),
					Controller: ptr.To(true),
				},
				{
					APIVersion: "apps/v1",
					Kind:       "Deployment",
					Name:       "deploy1",
					UID:        types.UID("5678"),
					Controller: ptr.To(false),
				},
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

	actualOwnerRefs := objB.GetOwnerReferences()
	// CopyReferences should copy all refs and set Controller to false
	expectedOwnerRefs := []metav1.OwnerReference{
		{
			APIVersion: "v1",
			Kind:       "Pod",
			Name:       "pod1",
			UID:        types.UID("1234"),
			Controller: ptr.To(false), // Changed from true to false
		},
		{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       "deploy1",
			UID:        types.UID("5678"),
			Controller: ptr.To(false),
		},
	}
	assert.Equal(t, expectedOwnerRefs, actualOwnerRefs)
}

func TestNativeRevisionMetadata_ReferSameObjectEdgeCases(t *testing.T) {
	t.Parallel()

	// Test via IsCurrent with various owner reference configurations.
	// Note: The owner is always a ConfigMap, so its GVK is always v1/ConfigMap (core group).
	// We test referSameObject by varying the ref's GVK fields.
	testCases := []struct {
		name          string
		ownerName     string
		ownerUID      types.UID
		refAPIVer     string
		refKind       string
		refName       string
		refUID        types.UID
		expectedMatch bool
	}{
		{
			name:          "same objects",
			ownerName:     "cm1",
			ownerUID:      types.UID("1234"),
			refAPIVer:     "v1",
			refKind:       "ConfigMap",
			refName:       "cm1",
			refUID:        types.UID("1234"),
			expectedMatch: true,
		},
		{
			name:          "different groups - core vs apps",
			ownerName:     "cm1",
			ownerUID:      types.UID("1234"),
			refAPIVer:     "apps/v1",
			refKind:       "ConfigMap",
			refName:       "cm1",
			refUID:        types.UID("1234"),
			expectedMatch: false, // core group vs apps group
		},
		{
			name:          "same group different versions - both core",
			ownerName:     "cm1",
			ownerUID:      types.UID("1234"),
			refAPIVer:     "v1beta1", // core group with different version
			refKind:       "ConfigMap",
			refName:       "cm1",
			refUID:        types.UID("1234"),
			expectedMatch: true, // Same group (core), different versions should match
		},
		{
			name:          "different kinds",
			ownerName:     "cm1",
			ownerUID:      types.UID("1234"),
			refAPIVer:     "v1",
			refKind:       "Secret",
			refName:       "cm1",
			refUID:        types.UID("1234"),
			expectedMatch: false,
		},
		{
			name:          "different names",
			ownerName:     "cm1",
			ownerUID:      types.UID("1234"),
			refAPIVer:     "v1",
			refKind:       "ConfigMap",
			refName:       "cm2",
			refUID:        types.UID("1234"),
			expectedMatch: false,
		},
		{
			name:          "different UIDs",
			ownerName:     "cm1",
			ownerUID:      types.UID("1234"),
			refAPIVer:     "v1",
			refKind:       "ConfigMap",
			refName:       "cm1",
			refUID:        types.UID("5678"),
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
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: tc.refAPIVer,
							Kind:       tc.refKind,
							Name:       tc.refName,
							UID:        tc.refUID,
							Controller: ptr.To(true),
						},
					},
				},
			}

			m := NewNativeRevisionMetadata(owner, testScheme)
			result := m.IsCurrent(obj)
			assert.Equal(t, tc.expectedMatch, result)
		})
	}
}

func TestNativeRevisionMetadata_RemoveFromNotFound(t *testing.T) {
	t.Parallel()

	obj := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{
				{Name: "cm1", UID: types.UID("1234"), Kind: "ConfigMap", APIVersion: "v1"},
			},
		},
	}
	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cm2",
			UID:  types.UID("5678"),
		},
	}

	initialOwnerRefs := obj.GetOwnerReferences()
	m := NewNativeRevisionMetadata(owner, testScheme)
	m.RemoveFrom(obj)
	finalOwnerRefs := obj.GetOwnerReferences()

	assert.Equal(t, initialOwnerRefs, finalOwnerRefs)
}

func TestNativeRevisionMetadata_IsNamespaceAllowed(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		ownerNamespace string
		objNamespace   string
		expected       bool
	}{
		{
			name:           "same namespace",
			ownerNamespace: "test",
			objNamespace:   "test",
			expected:       true,
		},
		{
			name:           "different namespace",
			ownerNamespace: "test",
			objNamespace:   "other",
			expected:       false,
		},
		{
			name:           "cluster-scoped owner",
			ownerNamespace: "", // cluster-scoped
			objNamespace:   "any-namespace",
			expected:       true, // cluster-scoped owners allow any namespace
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

			m := NewNativeRevisionMetadata(owner, testScheme)
			result := m.IsNamespaceAllowed(obj)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestNativeRevisionMetadata_GetOwner(t *testing.T) {
	t.Parallel()

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-owner",
			Namespace: "test",
			UID:       types.UID("owner-uid"),
		},
	}

	m := NewNativeRevisionMetadata(owner, testScheme).(*NativeRevisionMetadata)
	assert.Equal(t, owner, m.GetOwner())
}

func TestNativeRevisionMetadata_PanicsOnEmptyUID(t *testing.T) {
	t.Parallel()

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "owner-without-uid",
			Namespace: "test",
			// UID is empty - not persisted to cluster
		},
	}

	assert.Panics(t, func() {
		_ = NewNativeRevisionMetadata(owner, testScheme)
	})
}
