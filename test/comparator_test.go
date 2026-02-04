//go:build integration

package boxcutter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"

	"pkg.package-operator.run/boxcutter/machinery"
	"pkg.package-operator.run/boxcutter/ownerhandling"
)

// sanitizeCompareResult ensures that the managed field list of kube-controller-manager is consistent.
//
// kube-controller-manager sometimes reports .metadata.annotations as managed on CI, sometimes not.
func sanitizeCompareResult(r *machinery.CompareResult) {
	for i := range r.OtherManagers {
		manager := r.OtherManagers[i]
		if manager.Manager == "kube-controller-manager" {
			manager.Fields.Insert(fieldpath.MakePathOrDie("metadata", "annotations"))
		}
	}
}

//nolint:maintidx
func TestComparator(t *testing.T) {
	os := ownerhandling.NewNative(Scheme)
	comp := machinery.NewComparator(
		os, DiscoveryClient, Scheme, fieldOwner)

	ctx := t.Context()
	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "owner",
			Namespace: "default",
		},
	}
	require.NoError(t, Client.Create(t.Context(), owner, client.FieldOwner(fieldOwner)))
	t.Cleanup(func() {
		//nolint:usetesting
		if err := Client.Delete(context.Background(), owner); err != nil {
			t.Error(err)
		}
	})

	// ConfigMap unstructured
	actualConfigMap := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "comp-test-1",
				"namespace": "default",
			},
			"data": map[string]interface{}{
				"test": "test",
			},
		},
	}
	desiredConfigMap := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "comp-test-1",
				"namespace": "default",
			},
			"data": map[string]interface{}{
				"test":  "testxx",
				"test1": "test1",
			},
		},
	}

	// Deployment unstructured
	actualDeployment := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      "comp-test-2",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"replicas": int64(1),
				"selector": map[string]interface{}{
					"matchLabels": map[string]interface{}{
						"app": "com-test-2",
					},
				},
				"template": map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]interface{}{
							"app": "com-test-2",
						},
					},
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "app",
								"image": "does-not-matter",
								"env": []interface{}{
									map[string]interface{}{
										"name":  "XXX",
										"value": "XXX",
									},
								},
							},
						},
					},
				},
			},
		},
	}
	desiredDeployment := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      "comp-test-2",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"replicas": int64(1),
				"selector": map[string]interface{}{
					"matchLabels": map[string]interface{}{
						"app": "com-test-2",
					},
				},
				"template": map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": map[string]interface{}{
							"app": "com-test-2",
						},
					},
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "app",
								"image": "still-does-not-matter",
								"env": []interface{}{
									map[string]interface{}{
										"name":  "XXX",
										"value": "XXX",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	tests := []struct {
		name string
		// object to create on the cluster
		actualObj  *unstructured.Unstructured
		desiredObj *unstructured.Unstructured
		updateFn   func(ctx context.Context, actualObj *unstructured.Unstructured) error
		report     string
	}{
		{
			name:       "ConfigMap",
			actualObj:  actualConfigMap.DeepCopy(),
			desiredObj: desiredConfigMap.DeepCopy(),
			report: `Comparison:
- Modified:
  .data.test
- Removed:
  .data.test1
`,
		},
		{
			name:       "Deployment",
			actualObj:  actualDeployment.DeepCopy(),
			desiredObj: desiredDeployment.DeepCopy(),
			report: `Other:
- "kube-controller-manager"
  .metadata.annotations
  .metadata.annotations.deployment.kubernetes.io/revision
  .status.conditions
  .status.observedGeneration
  .status.replicas
  .status.unavailableReplicas
  .status.updatedReplicas
  .status.conditions[type="Available"]
  .status.conditions[type="Progressing"]
  .status.conditions[type="Available"].lastTransitionTime
  .status.conditions[type="Available"].lastUpdateTime
  .status.conditions[type="Available"].message
  .status.conditions[type="Available"].reason
  .status.conditions[type="Available"].status
  .status.conditions[type="Available"].type
  .status.conditions[type="Progressing"].lastTransitionTime
  .status.conditions[type="Progressing"].lastUpdateTime
  .status.conditions[type="Progressing"].message
  .status.conditions[type="Progressing"].reason
  .status.conditions[type="Progressing"].status
  .status.conditions[type="Progressing"].type
Comparison:
- Added:
  .metadata.annotations.deployment.kubernetes.io/revision
  .spec.progressDeadlineSeconds
  .spec.revisionHistoryLimit
  .spec.strategy.type
  .spec.strategy.rollingUpdate.maxSurge
  .spec.strategy.rollingUpdate.maxUnavailable
  .spec.template.spec.dnsPolicy
  .spec.template.spec.restartPolicy
  .spec.template.spec.schedulerName
  .spec.template.spec.securityContext
  .spec.template.spec.terminationGracePeriodSeconds
  .spec.template.spec.containers[name="app"].imagePullPolicy
  .spec.template.spec.containers[name="app"].terminationMessagePath
  .spec.template.spec.containers[name="app"].terminationMessagePolicy
- Modified:
  .spec.template.spec.containers[name="app"].image
`,
		},
		{
			name:      "Deployment update conflict",
			actualObj: actualDeployment.DeepCopy(),
			updateFn: func(ctx context.Context, actualObj *unstructured.Unstructured) error {
				if err := unstructured.SetNestedField(actualObj.Object, int64(2), "spec", "replicas"); err != nil {
					return err
				}

				return Client.Update(ctx, actualObj)
			},
			desiredObj: desiredDeployment.DeepCopy(),
			report: `Conflicts:
- "test.test"
  .spec.replicas
Other:
- "kube-controller-manager"
  .metadata.annotations
  .metadata.annotations.deployment.kubernetes.io/revision
  .status.conditions
  .status.observedGeneration
  .status.replicas
  .status.unavailableReplicas
  .status.updatedReplicas
  .status.conditions[type="Available"]
  .status.conditions[type="Progressing"]
  .status.conditions[type="Available"].lastTransitionTime
  .status.conditions[type="Available"].lastUpdateTime
  .status.conditions[type="Available"].message
  .status.conditions[type="Available"].reason
  .status.conditions[type="Available"].status
  .status.conditions[type="Available"].type
  .status.conditions[type="Progressing"].lastTransitionTime
  .status.conditions[type="Progressing"].lastUpdateTime
  .status.conditions[type="Progressing"].message
  .status.conditions[type="Progressing"].reason
  .status.conditions[type="Progressing"].status
  .status.conditions[type="Progressing"].type
Comparison:
- Added:
  .metadata.annotations.deployment.kubernetes.io/revision
  .spec.progressDeadlineSeconds
  .spec.revisionHistoryLimit
  .spec.strategy.type
  .spec.strategy.rollingUpdate.maxSurge
  .spec.strategy.rollingUpdate.maxUnavailable
  .spec.template.spec.dnsPolicy
  .spec.template.spec.restartPolicy
  .spec.template.spec.schedulerName
  .spec.template.spec.securityContext
  .spec.template.spec.terminationGracePeriodSeconds
  .spec.template.spec.containers[name="app"].imagePullPolicy
  .spec.template.spec.containers[name="app"].terminationMessagePath
  .spec.template.spec.containers[name="app"].terminationMessagePolicy
- Modified:
  .spec.replicas
  .spec.template.spec.containers[name="app"].image
`,
		},
		{
			name:      "Deployment multiple managers",
			actualObj: actualDeployment.DeepCopy(),
			updateFn: func(ctx context.Context, actualObj *unstructured.Unstructured) error {
				// Forced conflict
				err := Client.Patch(ctx,
					actualObj, client.RawPatch(types.ApplyPatchType, []byte(
						`{"apiVersion":"apps/v1","kind":"Deployment","spec":{"replicas": 2}}`,
					)),
					client.FieldOwner("Hans"), client.ForceOwnership,
				)
				if err != nil {
					return err
				}

				return Client.Patch(ctx,
					actualObj, client.RawPatch(types.ApplyPatchType, []byte(
						`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"annotations": {"test":"test"}}}`,
					)),
					client.FieldOwner("Franz"),
				)
			},
			desiredObj: desiredDeployment.DeepCopy(),
			report: `Conflicts:
- "Hans"
  .spec.replicas
Other:
- "Franz"
  .metadata.annotations.test
- "kube-controller-manager"
  .metadata.annotations
  .metadata.annotations.deployment.kubernetes.io/revision
  .status.conditions
  .status.observedGeneration
  .status.replicas
  .status.unavailableReplicas
  .status.updatedReplicas
  .status.conditions[type="Available"]
  .status.conditions[type="Progressing"]
  .status.conditions[type="Available"].lastTransitionTime
  .status.conditions[type="Available"].lastUpdateTime
  .status.conditions[type="Available"].message
  .status.conditions[type="Available"].reason
  .status.conditions[type="Available"].status
  .status.conditions[type="Available"].type
  .status.conditions[type="Progressing"].lastTransitionTime
  .status.conditions[type="Progressing"].lastUpdateTime
  .status.conditions[type="Progressing"].message
  .status.conditions[type="Progressing"].reason
  .status.conditions[type="Progressing"].status
  .status.conditions[type="Progressing"].type
Comparison:
- Added:
  .metadata.annotations.deployment.kubernetes.io/revision
  .metadata.annotations.test
  .spec.progressDeadlineSeconds
  .spec.revisionHistoryLimit
  .spec.strategy.type
  .spec.strategy.rollingUpdate.maxSurge
  .spec.strategy.rollingUpdate.maxUnavailable
  .spec.template.spec.dnsPolicy
  .spec.template.spec.restartPolicy
  .spec.template.spec.schedulerName
  .spec.template.spec.securityContext
  .spec.template.spec.terminationGracePeriodSeconds
  .spec.template.spec.containers[name="app"].imagePullPolicy
  .spec.template.spec.containers[name="app"].terminationMessagePath
  .spec.template.spec.containers[name="app"].terminationMessagePolicy
- Modified:
  .spec.replicas
  .spec.template.spec.containers[name="app"].image
`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t,
				controllerutil.SetControllerReference(owner, test.actualObj, Scheme))

			ac := client.ApplyConfigurationFromUnstructured(test.actualObj)
			require.NoError(t,
				Client.Apply(ctx, ac, client.FieldOwner(fieldOwner)))
			t.Cleanup(func() {
				if err := Client.Delete(ctx, test.actualObj); err != nil {
					t.Error(err)
				}
			})

			if test.updateFn != nil {
				err := test.updateFn(ctx, test.actualObj)
				require.NoError(t, err)
			}

			if test.actualObj.GetKind() == "Deployment" {
				err := Waiter.WaitForCondition(ctx, test.actualObj, "Available", metav1.ConditionFalse)
				require.NoError(t, err)
			}

			r, err := comp.Compare(owner, test.desiredObj, test.actualObj)
			require.NoError(t, err)
			sanitizeCompareResult(&r)

			assert.Equal(t, test.report, r.String())
		})
	}
}
