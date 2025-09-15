//go:build integration

package boxcutter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
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

	// ConfigMap structured
	actualConfigMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "comp-test-1",
			Namespace: "default",
		},
		Data: map[string]string{
			"test": "test",
		},
	}
	desiredConfigMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "comp-test-1",
			Namespace: "default",
		},
		Data: map[string]string{
			"test":  "testxx",
			"test1": "test1",
		},
	}

	// Deployment structured
	actualDeployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "comp-test-2",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To[int32](1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "com-test-2",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "com-test-2",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "does-not-matter",
							Env: []corev1.EnvVar{
								{
									Name:  "XXX",
									Value: "XXX",
								},
							},
						},
					},
				},
			},
		},
	}
	desiredDeployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "comp-test-2",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To[int32](1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "com-test-2",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "com-test-2",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "still-does-not-matter",
							Env: []corev1.EnvVar{
								{
									Name:  "XXX",
									Value: "XXX",
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
		actualObj  client.Object
		desiredObj client.Object
		updateFn   func(ctx context.Context, actualObj client.Object) error
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
			updateFn: func(ctx context.Context, actualObj client.Object) error {
				obj := actualObj.(*appsv1.Deployment)
				obj.Spec.Replicas = ptr.To[int32](2)

				return Client.Update(ctx, obj)
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
			updateFn: func(ctx context.Context, actualObj client.Object) error {
				obj := actualObj.(*appsv1.Deployment)

				// Forced conflict
				err := Client.Patch(ctx,
					obj, client.RawPatch(client.Apply.Type(), []byte(
						`{"apiVersion":"apps/v1","kind":"Deployment","spec":{"replicas": 2}}`,
					)),
					client.FieldOwner("Hans"), client.ForceOwnership,
				)
				if err != nil {
					return err
				}

				return Client.Patch(ctx,
					obj, client.RawPatch(client.Apply.Type(), []byte(
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
			require.NoError(t,
				Client.Patch(ctx, test.actualObj, client.Apply, client.FieldOwner(fieldOwner)))
			t.Cleanup(func() {
				if err := Client.Delete(ctx, test.actualObj); err != nil {
					t.Error(err)
				}
			})

			if test.updateFn != nil {
				err := test.updateFn(ctx, test.actualObj)
				require.NoError(t, err)
			}

			if _, ok := test.actualObj.(*appsv1.Deployment); ok {
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
