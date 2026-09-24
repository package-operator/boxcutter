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

	"pkg.package-operator.run/boxcutter"
	"pkg.package-operator.run/boxcutter/machinery"
	"pkg.package-operator.run/boxcutter/ownerhandling"
	"pkg.package-operator.run/boxcutter/probing"
	"pkg.package-operator.run/boxcutter/validation"
)

// TestRevisionEngine_Teardown_ObserveAfterIncomplete verifies that with
// WithObserveAfterIncomplete, teardown keeps going past a phase that is still
// being torn down and observes the remaining phases read-only: their objects are
// NOT deleted out of dependency order, but their status is still reported.
func TestRevisionEngine_Teardown_ObserveAfterIncomplete(t *testing.T) {
	revOwner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rev-observe-test",
			Namespace: "default",
		},
	}

	probe := &stubProbe{status: probing.StatusTrue}
	obj1 := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "test-observe-obj-1",
				"namespace": "default",
			},
		},
	}
	obj2 := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "test-observe-obj-2",
				"namespace": "default",
			},
		},
	}

	comp := machinery.NewComparator(DiscoveryClient, Scheme, fieldOwner)
	oe := machinery.NewObjectEngine(
		Scheme, Client, Client, comp, fieldOwner, systemPrefix, "", nil,
	)
	pval := validation.NewNamespacedPhaseValidator(Client.RESTMapper(), Client)
	pe := machinery.NewPhaseEngine(oe, pval)
	rval := validation.NewRevisionValidator()
	re := machinery.NewRevisionEngine(pe, rval, Client)

	ctx := t.Context()

	err := Client.Create(ctx, revOwner)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, Client.Delete(context.Background(), revOwner))
	})

	os := ownerhandling.NewNative(Scheme)
	rev := boxcutter.NewRevisionWithOwner(
		"rev-1", 1,
		[]boxcutter.Phase{
			boxcutter.NewPhase("phase-1", []client.Object{obj1}),
			boxcutter.NewPhase("phase-2", []client.Object{obj2}),
		},
		revOwner, os,
	)

	// Roll out both phases.
	res, err := re.Reconcile(ctx, rev,
		boxcutter.WithObjectReconcileOptions(obj1, boxcutter.WithProbe(boxcutter.ProgressProbeType, probe)),
		boxcutter.WithObjectReconcileOptions(obj2, boxcutter.WithProbe(boxcutter.ProgressProbeType, probe)),
	)
	require.NoError(t, err)
	require.True(t, res.IsComplete(), "revision should roll out\n"+res.String())

	// Block phase-2 (torn down first, in reverse) on a finalizer so it stays active.
	err = Client.Patch(ctx, obj2, client.RawPatch(
		types.MergePatchType, []byte(`{"metadata":{"finalizers":["package-operator.run/stopstopstop"]}}`)))
	require.NoError(t, err)

	t.Cleanup(func() {
		// Unblock and finish teardown so nothing leaks.
		_ = Client.Patch(context.Background(), obj2, client.RawPatch(
			types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`)))
		_, err := re.Teardown(context.Background(), rev)
		require.NoError(t, err)
	})

	tres, err := re.Teardown(ctx, rev, boxcutter.WithObserveAfterIncomplete{})
	require.NoError(t, err)

	assert.False(t, tres.IsComplete(), "teardown blocked on phase-2\n"+tres.String())

	// phase-2 is actively being torn down.
	active, ok := tres.GetActivePhaseName()
	if assert.True(t, ok) {
		assert.Equal(t, "phase-2", active)
	}

	// phase-1 is still waiting for real teardown, but was observed this pass.
	assert.Equal(t, []string{"phase-1"}, tres.GetWaitingPhaseNames())
	assert.Len(t, tres.GetPhases(), 2, "both phases reported\n"+tres.String())

	// The key guarantee: phase-1's object was only observed, NOT deleted out of
	// order while phase-2 is still being torn down.
	cm := &corev1.ConfigMap{}
	require.NoError(t, Client.Get(
		ctx, client.ObjectKey{Name: "test-observe-obj-1", Namespace: "default"}, cm),
		"test-observe-obj-1 must not be deleted while observing")
}
