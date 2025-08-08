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
)

var testScheme = scheme.Scheme

func TestOwnerStrategyNative_RemoveOwner(t *testing.T) {
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

	s := NewNative(testScheme)
	s.RemoveOwner(owner, obj)

	assert.Equal(t, []metav1.OwnerReference{}, obj.GetOwnerReferences())
}

func TestOwnerStrategyNative_SetOwnerReference(t *testing.T) {
	t.Parallel()

	s := NewNative(testScheme)
	obj := &corev1.Secret{}
	cm1 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cm1",
			Namespace: obj.Namespace,
			UID:       types.UID("1234"),
		},
	}

	require.NoError(t, s.SetOwnerReference(cm1, obj))

	ownerRefs := obj.GetOwnerReferences()
	if assert.Len(t, ownerRefs, 1) {
		assert.Equal(t, cm1.Name, ownerRefs[0].Name)
		assert.Equal(t, "ConfigMap", ownerRefs[0].Kind)
		assert.Nil(t, ownerRefs[0].Controller)
	}

	cm2 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cm2",
			Namespace: obj.Namespace,
			UID:       types.UID("56789"),
		},
	}

	require.NoError(t, s.SetControllerReference(cm2, obj))
}

func TestOwnerStrategyNative_SetControllerReference(t *testing.T) {
	t.Parallel()

	s := NewNative(testScheme)
	obj := &corev1.Secret{}
	cm1 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cm1",
			Namespace: obj.Namespace,
			UID:       types.UID("1234"),
		},
	}

	err := s.SetControllerReference(cm1, obj)
	require.NoError(t, err)

	ownerRefs := obj.GetOwnerReferences()
	if assert.Len(t, ownerRefs, 1) {
		assert.Equal(t, cm1.Name, ownerRefs[0].Name)
		assert.Equal(t, "ConfigMap", ownerRefs[0].Kind)
		assert.True(t, *ownerRefs[0].Controller)
	}

	cm2 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cm2",
			Namespace: obj.Namespace,
			UID:       types.UID("56789"),
		},
	}
	err = s.SetControllerReference(cm2, obj)
	require.Error(t, err, controllerutil.AlreadyOwnedError{})

	s.ReleaseController(obj)

	err = s.SetControllerReference(cm2, obj)
	require.NoError(t, err)
	assert.True(t, s.IsOwner(cm1, obj))
	assert.True(t, s.IsOwner(cm2, obj))
}

func TestOwnerStrategyNative_IsController(t *testing.T) {
	t.Parallel()

	s := NewNative(testScheme)
	obj := &corev1.Secret{}
	cm1 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cm1",
			Namespace: obj.Namespace,
			UID:       types.UID("1234"),
		},
	}
	err := s.SetControllerReference(cm1, obj)
	require.NoError(t, err)

	cm2 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cm2",
			Namespace: obj.Namespace,
			UID:       types.UID("56789"),
		},
	}

	assert.True(t, s.IsController(cm1, obj))
	assert.False(t, s.IsController(cm2, obj))
}

func TestOwnerStrategyNative_IsOwner(t *testing.T) {
	t.Parallel()

	s := NewNative(testScheme)
	obj := &corev1.Secret{}
	cm1 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cm1",
			Namespace: obj.Namespace,
			UID:       types.UID("1234"),
		},
	}

	err := s.SetControllerReference(cm1, obj)
	require.NoError(t, err)

	cm2 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cm2",
			Namespace: obj.Namespace,
			UID:       types.UID("56789"),
		},
	}

	assert.True(t, s.IsOwner(cm1, obj))
	assert.False(t, s.IsOwner(cm2, obj))
}

func TestOwnerStrategyNative_ReleaseController(t *testing.T) {
	t.Parallel()

	s := NewNative(testScheme)
	obj := &corev1.Secret{}
	owner := &corev1.ConfigMap{}
	owner.Namespace = obj.Namespace

	err := s.SetControllerReference(owner, obj)
	require.NoError(t, err)

	ownerRefs := obj.GetOwnerReferences()
	if assert.Len(t, ownerRefs, 1) {
		assert.NotNil(t, ownerRefs[0].Controller)
	}

	s.ReleaseController(obj)
	ownerRefs = obj.GetOwnerReferences()

	if assert.Len(t, ownerRefs, 1) && assert.NotNil(t, ownerRefs[0].Controller) {
		assert.False(t, *ownerRefs[0].Controller)
	}
}

func TestOwnerStrategyNative_GetController(t *testing.T) {
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

	for i := range testCases {
		tc := testCases[i]
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := NewNative(testScheme)
			obj := &corev1.Secret{}
			obj.SetOwnerReferences(tc.ownerRefs)

			controller, found := s.GetController(obj)
			assert.Equal(t, tc.expectedFound, found)

			if tc.expectedFound {
				assert.Equal(t, tc.expectedController, controller)
			}
		})
	}
}

func TestOwnerStrategyNative_CopyOwnerReferences(t *testing.T) {
	t.Parallel()

	s := NewNative(testScheme)

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

	expectedOwnerRefs := objA.GetOwnerReferences()

	s.CopyOwnerReferences(objA, objB)

	actualOwnerRefs := objB.GetOwnerReferences()
	assert.Equal(t, expectedOwnerRefs, actualOwnerRefs)
}

func TestOwnerStrategyNative_ReferSameObjectEdgeCases(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		a        metav1.OwnerReference
		b        metav1.OwnerReference
		expected bool
	}{
		{
			name: "same objects",
			a: metav1.OwnerReference{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Name:       "cm1",
				UID:        types.UID("1234"),
			},
			b: metav1.OwnerReference{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Name:       "cm1",
				UID:        types.UID("1234"),
			},
			expected: true,
		},
		{
			name: "different groups same version",
			a: metav1.OwnerReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "deploy1",
				UID:        types.UID("1234"),
			},
			b: metav1.OwnerReference{
				APIVersion: "v1",
				Kind:       "Deployment",
				Name:       "deploy1",
				UID:        types.UID("1234"),
			},
			expected: false,
		},
		{
			name: "same group different versions",
			a: metav1.OwnerReference{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       "deploy1",
				UID:        types.UID("1234"),
			},
			b: metav1.OwnerReference{
				APIVersion: "apps/v1beta1",
				Kind:       "Deployment",
				Name:       "deploy1",
				UID:        types.UID("1234"),
			},
			expected: true,
		},
		{
			name: "different kinds",
			a: metav1.OwnerReference{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Name:       "cm1",
				UID:        types.UID("1234"),
			},
			b: metav1.OwnerReference{
				APIVersion: "v1",
				Kind:       "Secret",
				Name:       "cm1",
				UID:        types.UID("1234"),
			},
			expected: false,
		},
		{
			name: "different names",
			a: metav1.OwnerReference{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Name:       "cm1",
				UID:        types.UID("1234"),
			},
			b: metav1.OwnerReference{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Name:       "cm2",
				UID:        types.UID("1234"),
			},
			expected: false,
		},
		{
			name: "different UIDs",
			a: metav1.OwnerReference{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Name:       "cm1",
				UID:        types.UID("1234"),
			},
			b: metav1.OwnerReference{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Name:       "cm1",
				UID:        types.UID("5678"),
			},
			expected: false,
		},
	}

	for i := range testCases {
		tc := testCases[i]
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := NewNative(testScheme)
			result := s.referSameObject(tc.a, tc.b)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestOwnerStrategyNative_RemoveOwnerNotFound(t *testing.T) {
	t.Parallel()

	s := NewNative(testScheme)
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
	s.RemoveOwner(owner, obj)
	finalOwnerRefs := obj.GetOwnerReferences()

	assert.Equal(t, initialOwnerRefs, finalOwnerRefs)
}
