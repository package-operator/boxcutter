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
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/kube-openapi/pkg/spec3"
	"pkg.package-operator.run/boxcutter/machinery/ownerhandling"
)

const testFieldOwner = "test.testy"

func TestComparator(t *testing.T) {
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

		expectedConflictingFieldOwners []string
		expectedConflictingPaths       map[string]string
	}{
		{
			name:                           "Hans updated .data.test",
			desired:                        desiredNewFieldOwner,
			actual:                         actualNewFieldOwner,
			expectedConflictingFieldOwners: []string{"Hans"},
			expectedConflictingPaths: map[string]string{
				"Hans": ".data.test",
			},
		},
		{
			name:    "xxx",
			desired: pod.DeepCopy(),
			actual:  pod.DeepCopy(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			res, err := d.HasDiverged(owner, test.desired, test.actual)
			require.NoError(t, err)
			assert.Equal(t, test.expectedConflictingFieldOwners, res.ConflictingFieldOwners)
			for k, v := range test.expectedConflictingPaths {
				assert.Equal(t,
					v, res.ConflictingPathsByFieldOwner[k].String(),
				)
			}
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

		res, err := d.HasDiverged(owner, desiredValueChange, actualValueChange)
		require.NoError(t, err)
		// no conflicts
		assert.Empty(t, res.ConflictingFieldOwners)
		assert.Empty(t, res.ConflictingPathsByFieldOwner)
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
			cn, err := openAPICanonicalName(test.obj)
			require.NoError(t, err)
			assert.Equal(t, test.cn, cn)
		})
	}
}
