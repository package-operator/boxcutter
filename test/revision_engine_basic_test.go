//go:build integration

package boxcutter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"pkg.package-operator.run/boxcutter"
	"pkg.package-operator.run/boxcutter/machinery"
	"pkg.package-operator.run/boxcutter/ownerhandling"
	"pkg.package-operator.run/boxcutter/probing"
	"pkg.package-operator.run/boxcutter/validation"
)

func TestRevisionEngine(t *testing.T) {
	revOwner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rev-test",
			Namespace: "default",
		},
	}

	obj1Probe := &stubProbe{status: probing.StatusFalse, messages: []string{"nope"}}
	obj2Probe := &stubProbe{status: probing.StatusTrue}
	obj1 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "test-rev-obj-1",
				"namespace": "default",
			},
		},
	}
	obj2 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "test-rev-obj-2",
				"namespace": "default",
			},
		},
	}

	comp := machinery.NewComparator(DiscoveryClient, Scheme, fieldOwner)
	oe := machinery.NewObjectEngine(
		Scheme, Client, Client, comp, fieldOwner, systemPrefix,
	)
	pval := validation.NewNamespacedPhaseValidator(Client.RESTMapper(), Client)
	pe := machinery.NewPhaseEngine(oe, pval)
	rval := validation.NewRevisionValidator()
	re := machinery.NewRevisionEngine(pe, rval, Client)

	ctx := t.Context()

	// Owner has to be there first:
	err := Client.Create(ctx, revOwner)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, Client.Delete(context.Background(), revOwner))
	})

	os := ownerhandling.NewNative(Scheme)
	rev := boxcutter.NewRevisionWithOwner(
		"rev-1", 1,
		[]boxcutter.Phase{
			boxcutter.NewPhase(
				"phase-1",
				[]client.Object{obj1},
			),
			boxcutter.NewPhase(
				"phase-2",
				[]client.Object{obj2},
			),
		},
		revOwner, os,
	)

	// Test execution
	// --------------

	// 1st Run.
	res, err := re.Reconcile(ctx, rev,
		boxcutter.WithObjectReconcileOptions(obj1, boxcutter.WithProbe(boxcutter.ProgressProbeType, obj1Probe)),
		boxcutter.WithObjectReconcileOptions(obj2, boxcutter.WithProbe(boxcutter.ProgressProbeType, obj2Probe)),
	)
	require.NoError(t, err)

	assert.False(t, res.IsComplete(), "Revision should not be complete.")
	assert.True(t, res.InTransition(), "Revision should be in transition.")

	cm := &corev1.ConfigMap{}
	require.NoError(t, Client.Get(
		ctx, client.ObjectKey{Name: "test-rev-obj-1", Namespace: "default"}, cm),
		"test-rev-obj-1 should have been created")
	assert.True(t,
		errors.IsNotFound(
			Client.Get(ctx, client.ObjectKey{Name: "test-rev-obj-2", Namespace: "default"}, cm)),
		"test-rev-obj-2 should not have been created")

	// 2nd Run.
	obj1Probe.status = probing.StatusTrue
	obj1Probe.messages = nil
	res, err = re.Reconcile(ctx, rev)
	require.NoError(t, err)

	assert.True(t, res.IsComplete(), "Revision should be complete.")
	assert.False(t, res.InTransition(), "Revision should not be in transition.")
	assert.NoError(t, Client.Get(ctx, client.ObjectKey{Name: "test-rev-obj-1", Namespace: "default"}, cm),
		"test-rev-obj-1 should have been created")
	assert.NoError(t, Client.Get(ctx, client.ObjectKey{Name: "test-rev-obj-2", Namespace: "default"}, cm),
		"test-rev-obj-2 should have been created")

	// Teardown
	cmToStop := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rev-obj-2",
			Namespace: "default",
		},
	}
	err = Client.Patch(ctx, cmToStop, client.RawPatch(
		types.MergePatchType, []byte(`{"metadata":{"finalizers":["package-operator.run/stopstopstop"]}}`)))
	require.NoError(t, err)

	// First try.
	tres, err := re.Teardown(ctx, rev)
	require.NoError(t, err)

	// phase-2 is active
	phaseName, ok := tres.GetActivePhaseName()
	if assert.True(t, ok) {
		assert.Equal(t, "phase-2", phaseName)
	}

	assert.False(t, tres.IsComplete(), "Deletion is not complete\n"+tres.String())
	require.NoError(t, Client.Get(ctx, client.ObjectKey{Name: "test-rev-obj-1", Namespace: "default"}, cm),
		"test-rev-obj-1 should still be present")

	// Second Try.
	err = Client.Patch(ctx, cmToStop, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`)))
	require.NoError(t, err)

	tres, err = re.Teardown(ctx, rev)
	require.NoError(t, err)

	// phase-1 is active
	phaseName, ok = tres.GetActivePhaseName()
	if assert.True(t, ok) {
		assert.Equal(t, "phase-1", phaseName)
	}

	assert.False(t, tres.IsComplete(), "Deletion is not complete\n"+tres.String())

	// Third Try.
	tres, err = re.Teardown(ctx, rev)
	require.NoError(t, err)
	assert.True(t, tres.IsComplete(), "Deletion is complete\n"+tres.String())
}

type stubProbe struct {
	status   probing.Status
	messages []string
}

func (p *stubProbe) Probe(_ client.Object) probing.Result {
	return probing.Result{Status: p.status, Messages: p.messages}
}
