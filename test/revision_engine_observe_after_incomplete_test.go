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
	"sigs.k8s.io/controller-runtime/pkg/client"

	"pkg.package-operator.run/boxcutter"
	"pkg.package-operator.run/boxcutter/machinery"
	"pkg.package-operator.run/boxcutter/ownerhandling"
	"pkg.package-operator.run/boxcutter/probing"
	"pkg.package-operator.run/boxcutter/validation"
)

// TestRevisionEngine_ObserveAfterIncomplete verifies that with
// WithObserveAfterIncomplete the engine keeps reconciling phases after the first
// incomplete phase in read-only (paused) mode: downstream phase objects are not
// written, but their status is still reported.
func TestRevisionEngine_ObserveAfterIncomplete(t *testing.T) {
	revOwner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rev-pause-test",
			Namespace: "default",
		},
	}

	// phase-1's probe never passes, so phase-1 stays incomplete and gates the rollout.
	obj1Probe := &stubProbe{status: probing.StatusFalse, messages: []string{"nope"}}
	obj2Probe := &stubProbe{status: probing.StatusTrue}
	obj1 := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "test-pause-obj-1",
				"namespace": "default",
			},
		},
	}
	obj2 := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "test-pause-obj-2",
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

	res, err := re.Reconcile(ctx, rev,
		boxcutter.WithObserveAfterIncomplete{},
		boxcutter.WithObjectReconcileOptions(obj1, boxcutter.WithProbe(boxcutter.ProgressProbeType, obj1Probe)),
		boxcutter.WithObjectReconcileOptions(obj2, boxcutter.WithProbe(boxcutter.ProgressProbeType, obj2Probe)),
	)
	require.NoError(t, err)

	// Revision is incomplete because phase-1 has not rolled out.
	assert.False(t, res.IsComplete(), "Revision should not be complete.")
	assert.True(t, res.InTransition(), "Revision should be in transition.")

	// Unlike the default (stop-and-wait) behavior, phase-2 is still reported.
	require.Len(t, res.GetPhases(), 2, "both phases should be reported\n"+res.String())
	assert.Equal(t, "phase-2", res.GetPhases()[1].GetName())

	// phase-1 was reconciled normally and its object created.
	cm := &corev1.ConfigMap{}
	require.NoError(t, Client.Get(
		ctx, client.ObjectKey{Name: "test-pause-obj-1", Namespace: "default"}, cm),
		"test-pause-obj-1 should have been created")

	// phase-2 was reconciled paused (read-only): its object must NOT be written.
	assert.True(t,
		errors.IsNotFound(
			Client.Get(ctx, client.ObjectKey{Name: "test-pause-obj-2", Namespace: "default"}, cm)),
		"test-pause-obj-2 should not have been created while paused")

	t.Cleanup(func() {
		_, err := re.Teardown(context.Background(), rev)
		require.NoError(t, err)
	})
}
