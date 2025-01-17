//go:build integration

package boxcutter

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"pkg.package-operator.run/boxcutter/machinery"
	"pkg.package-operator.run/boxcutter/machinery/ownerhandling"
	bctypes "pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/machinery/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func TestRevisionEngine(t *testing.T) {
	revOwner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rev-test",
			Namespace: "default",
		},
	}

	obj1Probe := &stubProbe{success: false, messages: []string{"nope"}}
	obj2Probe := &stubProbe{success: true}
	rev := &bctypes.RevisionStandin{
		Name:     "rev-1",
		Owner:    revOwner,
		Revision: 1,
		Phases: []bctypes.Phase{
			&bctypes.PhaseStandin{
				Name: "phase-1",
				Objects: []bctypes.PhaseObject{
					{
						Object: &unstructured.Unstructured{
							Object: map[string]interface{}{
								"apiVersion": "v1",
								"kind":       "ConfigMap",
								"metadata": map[string]interface{}{
									"name":      "test-rev-obj-1",
									"namespace": "default",
								},
							},
						},
						Opts: []bctypes.ObjectOption{
							bctypes.WithProbe{Probe: obj1Probe},
						},
					},
				},
			},
			&bctypes.PhaseStandin{
				Name: "phase-2",
				Objects: []bctypes.PhaseObject{
					{
						Object: &unstructured.Unstructured{
							Object: map[string]interface{}{
								"apiVersion": "v1",
								"kind":       "ConfigMap",
								"metadata": map[string]interface{}{
									"name":      "test-rev-obj-2",
									"namespace": "default",
								},
							},
						},
						Opts: []bctypes.ObjectOption{
							bctypes.WithProbe{Probe: obj2Probe},
						},
					},
				},
			},
		},
	}

	os := ownerhandling.NewNative(Scheme)
	comp := machinery.NewComparator(os, DiscoveryClient, Scheme, fieldOwner)
	oe := machinery.NewObjectEngine(
		Scheme, &noopCache{Reader: Client}, Client,
		Client, os, comp, fieldOwner, systemPrefix,
	)
	pval := validation.NewNamespacedPhaseValidator(Client.RESTMapper(), Client)
	pe := machinery.NewPhaseEngine(oe, pval)
	rval := validation.NewRevisionValidator()
	anchorObjectManager := &anchorObjectManager{
		c:      Client,
		scheme: Scheme,
	}
	re := machinery.NewRevisionEngine(pe, rval, Client, anchorObjectManager)

	ctx := context.Background()

	// Owner has to be there first:
	err := Client.Create(ctx, revOwner)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, Client.Delete(ctx, revOwner))
	})

	// Test execution
	// --------------

	// 1st Run.
	res, err := re.Reconcile(ctx, rev)
	require.NoError(t, err)

	assert.False(t, res.IsComplete(), "Revision should not be complete.")
	assert.True(t, res.InTransistion(), "Revision should be in transition.")

	cm := &corev1.ConfigMap{}
	assert.NoError(t, Client.Get(ctx, client.ObjectKey{Name: "test-rev-obj-1", Namespace: "default"}, cm),
		"test-rev-obj-1 should have been created")
	assert.True(t,
		errors.IsNotFound(
			Client.Get(ctx, client.ObjectKey{Name: "test-rev-obj-2", Namespace: "default"}, cm)),
		"test-rev-obj-2 should not have been created")

	// 2nd Run.
	obj1Probe.success = true
	obj1Probe.messages = nil
	res, err = re.Reconcile(ctx, rev)
	require.NoError(t, err)

	assert.True(t, res.IsComplete(), "Revision should be complete.")
	assert.False(t, res.InTransistion(), "Revision should not be in transition.")
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
	err = Client.Patch(ctx, cmToStop, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":["package-operator.run/stopstopstop"]}}`)))
	require.NoError(t, err)

	// First try.
	tres, err := re.Teardown(ctx, rev)
	require.NoError(t, err)

	assert.False(t, tres.IsComplete(), "Deletion is not complete")
	assert.NoError(t, Client.Get(ctx, client.ObjectKey{Name: "test-rev-obj-1", Namespace: "default"}, cm),
		"test-rev-obj-1 should still be present")

	// Second Try.
	err = Client.Patch(ctx, cmToStop, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":[]}}`)))
	require.NoError(t, err)

	tres, err = re.Teardown(ctx, rev)
	require.NoError(t, err)

	assert.True(t, res.IsComplete(), "Deletion is complete")
}

type stubProbe struct {
	success  bool
	messages []string
}

func (p *stubProbe) Probe(obj client.Object) (success bool, messages []string) {
	return p.success, p.messages
}

type anchorObjectManager struct {
	c      client.Client
	scheme *runtime.Scheme
}

func (re *anchorObjectManager) EnsureFor(
	ctx context.Context,
	owner client.Object,
	childs []client.Object,
) error {
	anchor, err := re.ensure(ctx, owner)
	if err != nil {
		return err
	}

	for _, child := range childs {
		if err := controllerutil.SetOwnerReference(anchor, child, re.scheme); err != nil {
			return fmt.Errorf("add anchor ownerref: %w", err)
		}
	}
	return nil
}

func (re *anchorObjectManager) ensure(ctx context.Context, owner client.Object) (client.Object, error) {
	cm := &corev1.ConfigMap{ // TODO: Needs cluster-scoped custom API.
		ObjectMeta: metav1.ObjectMeta{
			Name:      string(owner.GetUID()),
			Namespace: owner.GetNamespace(),
		},
	}
	err := re.c.Get(ctx, client.ObjectKeyFromObject(cm), cm)
	if errors.IsNotFound(err) {
		if err := re.c.Create(ctx, cm); err != nil {
			return cm, nil
		}
	}
	if err != nil {
		return cm, nil
	}

	return cm, nil
}

func (re *anchorObjectManager) RemoveFor(
	ctx context.Context,
	owner client.Object,
) error {
	cm := &corev1.ConfigMap{ // TODO: Needs cluster-scoped custom API.
		ObjectMeta: metav1.ObjectMeta{
			Name:      string(owner.GetUID()),
			Namespace: owner.GetNamespace(),
		},
	}
	if err := re.c.Delete(ctx, cm); err != nil || errors.IsNotFound(err) {
		return nil
	}
	return nil
}
