package machinery

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/kube-openapi/pkg/spec3"
	"pkg.package-operator.run/boxcutter/machinery/ownerhandling"
)

const testFieldOwner = "test.testy"

func TestComparator_Unstructured(t *testing.T) {
	t.Parallel()

	testOAPISchema, err := os.ReadFile("testdata/schemas.json")
	require.NoError(t, err)

	oapi := &spec3.OpenAPI{}
	require.NoError(t, oapi.UnmarshalJSON(testOAPISchema))

	a := &dummyOpenAPIAccessor{
		openAPI: oapi,
	}
	n := ownerhandling.NewNative(scheme.Scheme)
	d := &Comparator{
		ownerStrategy:   n,
		openAPIAccessor: a,
		fieldOwner:      testFieldOwner,
	}

	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			UID:       "12345-678",
			Name:      "owner",
			Namespace: "test",
		},
	}

	// Test Case 1
	// Another actor has updated .data.test and the field owner has changed.
	desiredNewFieldOwner := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "t",
				"namespace": "test",
			},
			"data": map[string]interface{}{
				"test": "test123",
			},
		},
	}

	actualNewFieldOwner := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "t",
				"namespace": "test",
				"managedFields": []interface{}{
					map[string]interface{}{
						"apiVersion": "v1",
						"fieldsType": "FieldsV1",
						"fieldsV1":   map[string]interface{}{},
						"manager":    testFieldOwner,
						"operation":  "Apply",
					},
					map[string]interface{}{
						"apiVersion": "v1",
						"fieldsType": "FieldsV1",
						"fieldsV1": map[string]interface{}{
							"f:data": map[string]interface{}{
								"f:test": map[string]interface{}{},
							},
						},
						"manager":   "Hans",
						"operation": "Apply",
					},
				},
			},
			"data": map[string]interface{}{
				"test": "test123",
			},
		},
	}
	err = n.SetControllerReference(owner, actualNewFieldOwner)
	require.NoError(t, err)

	// Test Case 2
	pod := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":      "t",
				"namespace": "test",
				"managedFields": []interface{}{
					map[string]interface{}{
						"apiVersion": "v1",
						"fieldsType": "FieldsV1",
						"fieldsV1": map[string]interface{}{
							"f:spec": map[string]interface{}{
								"f:containers": map[string]interface{}{
									`k:{"name":"manager"}`: map[string]interface{}{
										".": map[string]interface{}{},
										"f:env": map[string]interface{}{
											".": map[string]interface{}{},
											`k:{"name":"TEST"}`: map[string]interface{}{
												".":       map[string]interface{}{},
												"f:name":  map[string]interface{}{},
												"f:value": map[string]interface{}{},
											},
										},
										"f:ports": map[string]interface{}{
											".": map[string]interface{}{},
											`k:{"containerPort":8080,"protocol":"TCP"}`: map[string]interface{}{
												".":               map[string]interface{}{},
												"f:name":          map[string]interface{}{},
												"f:protocol":      map[string]interface{}{},
												"f:containerPort": map[string]interface{}{},
											},
										},
									},
								},
							},
						},
						"manager":   testFieldOwner,
						"operation": "Apply",
					},
				},
			},
			"spec": map[string]interface{}{
				"containers": []interface{}{
					map[string]interface{}{
						"name": "manager",
						"ports": []interface{}{
							map[string]interface{}{
								"containerPort": float64(8080),
								"protocol":      "TCP",
							},
						},
						"env": []interface{}{
							map[string]interface{}{
								"name":  "TEST",
								"value": "xxx",
							},
						},
					},
				},
			},
		},
	}
	err = n.SetControllerReference(owner, pod)
	require.NoError(t, err)

	tests := []struct {
		name    string
		desired *unstructured.Unstructured
		actual  *unstructured.Unstructured

		expectedReport string
	}{
		{
			name:    "Hans updated .data.test",
			desired: desiredNewFieldOwner,
			actual:  actualNewFieldOwner,
			expectedReport: `Conflicts:
- "Hans"
  .data.test
`,
		},
		{
			name:           "Pod Compare",
			desired:        pod.DeepCopy(),
			actual:         pod.DeepCopy(),
			expectedReport: ``,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			res, err := d.Compare(owner, test.desired, test.actual)
			require.NoError(t, err)

			assert.Equal(t, test.expectedReport, res.String())
			if res.Comparison != nil {
				assert.True(t, res.Comparison.IsSame(), res.Comparison.String())
			}
		})
	}

	t.Run("divergence on field values", func(t *testing.T) {
		t.Parallel()

		desiredValueChange := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata": map[string]interface{}{
					"name":      "t",
					"namespace": "test",
				},
				"data": map[string]interface{}{
					"test": "test123",
				},
			},
		}

		actualValueChange := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata": map[string]interface{}{
					"name":      "t",
					"namespace": "test",
					"managedFields": []interface{}{
						map[string]interface{}{
							"apiVersion": "v1",
							"fieldsType": "FieldsV1",
							"fieldsV1": map[string]interface{}{
								"f:data": map[string]interface{}{
									"f:test": map[string]interface{}{},
								},
							},
							"manager":   testFieldOwner,
							"operation": "Apply",
						},
					},
				},
				"data": map[string]interface{}{
					"test": "test1234",
				},
			},
		}
		err = n.SetControllerReference(owner, actualValueChange)
		require.NoError(t, err)

		res, err := d.Compare(owner, desiredValueChange, actualValueChange)
		require.NoError(t, err)
		// no conflicts
		assert.Empty(t, res.ConflictingMangers)
		// But a modification
		assert.Equal(t, "- Modified Fields:\n.data.test\n", res.Comparison.String())
	})
}

func TestComparator_Structured(t *testing.T) {
	t.Parallel()

	testOAPISchema, err := os.ReadFile("testdata/schemas.json")
	require.NoError(t, err)

	oapi := &spec3.OpenAPI{}
	require.NoError(t, oapi.UnmarshalJSON(testOAPISchema))

	a := &dummyOpenAPIAccessor{
		openAPI: oapi,
	}
	n := ownerhandling.NewNative(scheme.Scheme)
	d := &Comparator{
		ownerStrategy:   n,
		openAPIAccessor: a,
		fieldOwner:      testFieldOwner,
	}

	now := metav1.Now()
	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			UID:               "12345-678",
			Name:              "owner",
			Namespace:         "test",
			CreationTimestamp: now,
		},
	}

	// Test Case 1
	// Another actor has updated .data.test and the field owner has changed.
	desiredNewFieldOwner := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "t",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"test": []byte("test123"),
		},
	}

	actualNewFieldOwner := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:              "t",
			Namespace:         "test",
			CreationTimestamp: now,
			UID:               types.UID("xxx"),
			ResourceVersion:   "xxx",
			Generation:        3,
			ManagedFields: []metav1.ManagedFieldsEntry{
				{
					APIVersion: "v1",
					Manager:    testFieldOwner,
					Operation:  metav1.ManagedFieldsOperationApply,
					FieldsType: "FieldsV1",
					FieldsV1: &metav1.FieldsV1{
						Raw: []byte(`{}`),
					},
				},
				{
					APIVersion: "v1",
					Manager:    "Hans",
					Operation:  metav1.ManagedFieldsOperationApply,
					FieldsType: "FieldsV1",
					FieldsV1: &metav1.FieldsV1{
						Raw: []byte(`{"f:data":{"f:test":{}}}`),
					},
				},
			},
		},
		Data: map[string][]byte{
			"test": []byte("test123"),
		},
	}
	err = n.SetControllerReference(owner, actualNewFieldOwner)
	require.NoError(t, err)

	// Test Case 2
	desiredPod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "t",
			Namespace: "test",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "manager",
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: 8080,
							Protocol:      corev1.ProtocolTCP,
						},
					},
					Env: []corev1.EnvVar{
						{
							Name:  "TEST",
							Value: "xxx",
						},
					},
				},
			},
		},
	}

	actualPod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:              "t",
			Namespace:         "test",
			CreationTimestamp: now,
			ManagedFields: []metav1.ManagedFieldsEntry{
				{
					APIVersion: "v1",
					Manager:    testFieldOwner,
					Operation:  metav1.ManagedFieldsOperationApply,
					FieldsType: "FieldsV1",
					FieldsV1: &metav1.FieldsV1{
						Raw: []byte(`{
    "f:spec": {
        "f:containers": {
            "k:{\"name\":\"manager\"}": {
                ".": {},
                "f:env": {
                    ".": {},
                    "k:{\"name\":\"TEST\"}": {
                        ".": {},
                        "f:name": {},
                        "f:value": {}
                    }
                },
                "f:ports": {
                    ".": {},
                    "k:{\"containerPort\":8080,\"protocol\":\"TCP\"}": {
                        ".": {},
                        "f:name": {},
                        "f:protocol": {},
                        "f:containerPort": {}
                    }
                }
            }
        }
    }
}`),
					},
				},
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Resources: corev1.ResourceRequirements{},
					Name:      "manager",
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: 8080,
							Protocol:      corev1.ProtocolTCP,
						},
					},
					Env: []corev1.EnvVar{
						{
							Name:  "TEST",
							Value: "xxx",
						},
					},
				},
			},
		},
	}
	err = n.SetControllerReference(owner, actualPod)
	require.NoError(t, err)

	tests := []struct {
		name    string
		desired Object
		actual  Object

		expectedReport string
	}{
		{
			name:    "Hans updated .data.test",
			desired: desiredNewFieldOwner,
			actual:  actualNewFieldOwner,
			expectedReport: `Conflicts:
- "Hans"
  .data.test
`,
		},
		{
			name:           "Pod no update",
			desired:        desiredPod.DeepCopy(),
			actual:         actualPod.DeepCopy(),
			expectedReport: ``,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			res, err := d.Compare(owner, test.desired, test.actual)
			require.NoError(t, err)
			if res.Comparison != nil {
				assert.True(t, res.Comparison.IsSame(), res.Comparison.String())
			}
		})
	}

	t.Run("divergence on field values", func(t *testing.T) {
		t.Parallel()

		desiredValueChange := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata": map[string]interface{}{
					"name":      "t",
					"namespace": "test",
				},
				"data": map[string]interface{}{
					"test": "test123",
				},
			},
		}

		actualValueChange := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata": map[string]interface{}{
					"name":      "t",
					"namespace": "test",
					"managedFields": []interface{}{
						map[string]interface{}{
							"apiVersion": "v1",
							"fieldsType": "FieldsV1",
							"fieldsV1": map[string]interface{}{
								"f:data": map[string]interface{}{
									"f:test": map[string]interface{}{},
								},
							},
							"manager":   testFieldOwner,
							"operation": "Apply",
						},
					},
				},
				"data": map[string]interface{}{
					"test": "test1234",
				},
			},
		}
		err = n.SetControllerReference(owner, actualValueChange)
		require.NoError(t, err)

		res, err := d.Compare(owner, desiredValueChange, actualValueChange)
		require.NoError(t, err)
		// no conflicts
		assert.Empty(t, res.ConflictingMangers)
		// But a modification
		assert.Equal(t, "- Modified Fields:\n.data.test\n", res.Comparison.String())
	})
}

type dummyOpenAPIAccessor struct {
	openAPI *spec3.OpenAPI
}

func (a *dummyOpenAPIAccessor) Get(_ schema.GroupVersion) (*spec3.OpenAPI, error) {
	return a.openAPI, nil
}

func Test_openAPICanonicalName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		obj  unstructured.Unstructured
		cn   string
	}{
		{
			name: "Pod",
			obj: unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Pod",
				},
			},
			cn: "io.k8s.api.core.v1.Pod",
		},
		{
			name: "Secret",
			obj: unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Secret",
				},
			},
			cn: "io.k8s.api.core.v1.Secret",
		},
		{
			name: "Deployment",
			obj: unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
				},
			},
			cn: "io.k8s.api.apps.v1.Deployment",
		},
		{
			name: "RoleBinding",
			obj: unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "rbac.authorization.k8s.io/v1",
					"kind":       "RoleBinding",
				},
			},
			cn: "io.k8s.api.rbac.v1.RoleBinding",
		},
		{
			name: "PKO CRD",
			obj: unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "package-operator.run/v1alpha1",
					"kind":       "Package",
				},
			},
			cn: "run.package-operator.v1alpha1.Package",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cn, err := openAPICanonicalName(&test.obj)
			require.NoError(t, err)
			assert.Equal(t, test.cn, cn)
		})
	}
}
