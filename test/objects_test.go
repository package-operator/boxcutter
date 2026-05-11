//go:build integration

package boxcutter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	machinerytypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"pkg.package-operator.run/boxcutter/machinery"
	"pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/ownerhandling"
)

func TestObjectEngine(t *testing.T) {
	os := ownerhandling.NewNative(Scheme)
	comp := machinery.NewComparator(DiscoveryClient, Scheme, fieldOwner)
	oe := machinery.NewObjectEngine(
		Scheme, Client, Client, comp, fieldOwner, systemPrefix, "", nil,
	)

	ctx := t.Context()
	owner := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "oe-owner",
				"namespace": "default",
			},
		},
	}
	require.NoError(t, Client.Create(ctx, owner, client.FieldOwner(fieldOwner)))
	t.Cleanup(func() {
		if err := Client.Delete(context.Background(), owner); err != nil {
			t.Error(err)
		}
	})

	configMap := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "oe-test",
				"namespace": "default",
			},
			"data": map[string]any{
				"test1": "test",
				"test2": "test",
			},
		},
	}

	// Creation
	res, err := oe.Reconcile(ctx, 1, configMap, types.WithOwner(owner, os))
	require.NoError(t, err)
	assert.Equal(t, `Object ConfigMap.v1 default/oe-test
Action: "Created"
`, res.String())
	assert.Equal(t, machinery.ActionCreated, res.Action())
	assert.True(t, res.IsComplete(), "IsComplete")
	assert.False(t, res.IsPaused(), "IsPaused")

	// Idle
	res, err = oe.Reconcile(ctx, 1, configMap, types.WithOwner(owner, os))
	require.NoError(t, err)
	assert.Equal(t, `Object ConfigMap.v1 default/oe-test
Action: "Idle"
`, res.String())
	assert.True(t, res.IsComplete(), "IsComplete")
	assert.False(t, res.IsPaused(), "IsPaused")

	// Add other participant.
	err = Client.Patch(ctx,
		configMap.DeepCopy(),
		client.RawPatch(machinerytypes.ApplyYAMLPatchType, []byte(
			`{"apiVersion":"v1","kind":"ConfigMap","data":{"test5": "xxx"}}`,
		)),
		client.FieldOwner("Franz"),
	)
	require.NoError(t, err)

	// Idle with other participant.
	res, err = oe.Reconcile(ctx, 1, configMap, types.WithOwner(owner, os))
	require.NoError(t, err)
	assert.Equal(t, `Object ConfigMap.v1 default/oe-test
Action: "Idle"
Other:
- "Franz"
  .data.test5
Comparison:
- Added:
  .data.test5
`, res.String())
	assert.True(t, res.IsComplete(), "IsComplete")
	assert.False(t, res.IsPaused(), "IsPaused")

	// Update with other participant.
	configMap.SetAnnotations(map[string]string{
		"my-annotation": "test",
	})

	err = unstructured.SetNestedStringMap(configMap.Object, map[string]string{
		"test1":    "new-value",
		"new-test": "new-value",
	}, "data")
	require.NoError(t, err)

	res, err = oe.Reconcile(ctx, 1, configMap, types.WithOwner(owner, os))
	require.NoError(t, err)
	assert.Equal(t, `Object ConfigMap.v1 default/oe-test
Action: "Updated"
Other:
- "Franz"
  .data.test5
Comparison:
- Added:
  .data.test2
  .data.test5
- Modified:
  .data.test1
- Removed:
  .data.new-test
  .metadata.annotations.my-annotation
`, res.String())
	assert.True(t, res.IsComplete(), "IsComplete")
	assert.False(t, res.IsPaused(), "IsPaused")

	// Teardown is a two step process at the moment.
	gone, err := oe.Teardown(ctx, 1, configMap, types.WithOwner(owner, os))
	require.NoError(t, err)
	assert.False(t, gone)

	gone, err = oe.Teardown(ctx, 1, configMap, types.WithOwner(owner, os))
	require.NoError(t, err)
	assert.True(t, gone)
}

func TestObjectEnginePaused(t *testing.T) {
	os := ownerhandling.NewNative(Scheme)
	comp := machinery.NewComparator(DiscoveryClient, Scheme, fieldOwner)
	oe := machinery.NewObjectEngine(
		Scheme, Client, Client, comp, fieldOwner, systemPrefix, "", nil,
	)

	ctx := t.Context()
	owner := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "oe-owner-paused",
				"namespace": "default",
			},
		},
	}
	require.NoError(t, Client.Create(ctx, owner, client.FieldOwner(fieldOwner)))
	t.Cleanup(func() {
		if err := Client.Delete(context.Background(), owner); err != nil {
			t.Error(err)
		}
	})

	configMap := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "oe-test-paused",
				"namespace": "default",
			},
			"data": map[string]any{
				"test1": "test",
				"test2": "test",
			},
		},
	}
	originalConfigMap := configMap.DeepCopy()

	// Creation Paused
	res, err := oe.Reconcile(ctx, 1, configMap, types.WithPaused{}, types.WithOwner(owner, os))
	require.NoError(t, err)
	assert.Equal(t, `Object ConfigMap.v1 default/oe-test-paused
Action (PAUSED): "Created"
`, res.String())
	assert.False(t, res.IsComplete(), "IsComplete")
	assert.True(t, res.IsPaused(), "IsPaused")

	cmShouldNotExist := &unstructured.Unstructured{}
	cmShouldNotExist.SetGroupVersionKind(configMap.GroupVersionKind())
	err = Client.Get(ctx, client.ObjectKeyFromObject(configMap), cmShouldNotExist)
	require.True(t, apierrors.IsNotFound(err), "Object should not exist after paused create action")

	// Creation Not Paused
	res, err = oe.Reconcile(ctx, 1, configMap, types.WithOwner(owner, os))
	require.NoError(t, err)
	assert.Equal(t, `Object ConfigMap.v1 default/oe-test-paused
Action: "Created"
`, res.String())
	assert.True(t, res.IsComplete(), "IsComplete")
	assert.False(t, res.IsPaused(), "IsPaused")

	// Idle Paused
	res, err = oe.Reconcile(ctx, 1, configMap, types.WithPaused{}, types.WithOwner(owner, os))
	require.NoError(t, err)
	assert.Equal(t, `Object ConfigMap.v1 default/oe-test-paused
Action (PAUSED): "Idle"
`, res.String())
	assert.True(t, res.IsComplete(), "IsComplete")
	assert.True(t, res.IsPaused(), "IsPaused")

	// Update Paused.
	configMap.SetAnnotations(map[string]string{
		"my-annotation": "test",
	})

	err = unstructured.SetNestedStringMap(configMap.Object, map[string]string{
		"test1":    "new-value",
		"new-test": "new-value",
	}, "data")
	require.NoError(t, err)

	res, err = oe.Reconcile(ctx, 1, configMap, types.WithPaused{}, types.WithOwner(owner, os))
	require.NoError(t, err)
	assert.Equal(t, `Object ConfigMap.v1 default/oe-test-paused
Action (PAUSED): "Updated"
Comparison:
- Added:
  .data.test2
- Modified:
  .data.test1
- Removed:
  .data.new-test
  .metadata.annotations.my-annotation
`, res.String())
	assert.False(t, res.IsComplete(), "IsComplete")
	assert.True(t, res.IsPaused(), "IsPaused")

	cmNotUpdated := &unstructured.Unstructured{}
	cmNotUpdated.SetGroupVersionKind(configMap.GroupVersionKind())
	err = Client.Get(ctx, client.ObjectKeyFromObject(configMap), cmNotUpdated)
	require.NoError(t, err)

	originalData, _, _ := unstructured.NestedStringMap(originalConfigMap.Object, "data")
	currentData, _, _ := unstructured.NestedStringMap(cmNotUpdated.Object, "data")
	assert.Equal(t, originalData, currentData)
	assert.Equal(t, originalConfigMap.GetAnnotations()["my-annotation"], cmNotUpdated.GetAnnotations()["my-annotation"])

	// Teardown is a two step process at the moment.
	gone, err := oe.Teardown(ctx, 1, configMap, types.WithOwner(owner, os))
	require.NoError(t, err)
	assert.False(t, gone)

	gone, err = oe.Teardown(ctx, 1, configMap, types.WithOwner(owner, os))
	require.NoError(t, err)
	assert.True(t, gone)
}

func TestObjectEngineProbing(t *testing.T) {
	os := ownerhandling.NewNative(Scheme)
	comp := machinery.NewComparator(DiscoveryClient, Scheme, fieldOwner)
	oe := machinery.NewObjectEngine(
		Scheme, Client, Client, comp, fieldOwner, systemPrefix, "", nil,
	)

	ctx := t.Context()
	owner := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "oe-owner-probing",
				"namespace": "default",
			},
		},
	}
	require.NoError(t, Client.Create(ctx, owner, client.FieldOwner(fieldOwner)))
	t.Cleanup(func() {
		if err := Client.Delete(context.Background(), owner); err != nil {
			t.Error(err)
		}
	})

	probeSuccess := &stubProbe{status: types.ProbeStatusTrue, messages: []string{"works!"}}
	probeFailed := &stubProbe{status: types.ProbeStatusFalse, messages: []string{"does not work!"}}
	probeUnknown := &stubProbe{status: types.ProbeStatusUnknown, messages: []string{"no clue!"}}

	configMap := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "oe-test-probing",
				"namespace": "default",
			},
			"data": map[string]any{
				"test1": "test",
				"test2": "test",
			},
		},
	}
	// Creation progress probe fails
	res, err := oe.Reconcile(
		ctx, 1, configMap,
		types.WithProbe(types.ProgressProbeType, probeFailed),
		types.WithProbe("other", probeSuccess),
		types.WithOwner(owner, os),
	)
	require.NoError(t, err)
	assert.Equal(t, `Object ConfigMap.v1 default/oe-test-probing
Action: "Created"
Probes:
- Progress: Failed
  - does not work!
- other: Succeeded
  - works!
`, res.String())
	assert.False(t, res.IsComplete(), "IsComplete")
	assert.False(t, res.IsPaused(), "IsPaused")

	// Idle progress probe unknown
	res, err = oe.Reconcile(
		ctx, 1, configMap,
		types.WithProbe(types.ProgressProbeType, probeUnknown),
		types.WithProbe("other", probeSuccess),
		types.WithOwner(owner, os),
	)
	require.NoError(t, err)
	assert.Equal(t, `Object ConfigMap.v1 default/oe-test-probing
Action: "Idle"
Probes:
- Progress: Unknown
  - no clue!
- other: Succeeded
  - works!
`, res.String())
	assert.False(t, res.IsComplete(), "IsComplete")
	assert.False(t, res.IsPaused(), "IsPaused")

	// Idle progress probe success
	res, err = oe.Reconcile(
		ctx, 1, configMap,
		types.WithProbe(types.ProgressProbeType, probeSuccess),
		types.WithProbe("other", probeSuccess),
		types.WithOwner(owner, os),
	)
	require.NoError(t, err)
	assert.Equal(t, `Object ConfigMap.v1 default/oe-test-probing
Action: "Idle"
Probes:
- Progress: Succeeded
  - works!
- other: Succeeded
  - works!
`, res.String())
	assert.True(t, res.IsComplete(), "IsComplete")
	assert.False(t, res.IsPaused(), "IsPaused")

	// Teardown is a two step process at the moment.
	gone, err := oe.Teardown(ctx, 1, configMap, types.WithOwner(owner, os))
	require.NoError(t, err)
	assert.False(t, gone)

	gone, err = oe.Teardown(ctx, 1, configMap, types.WithOwner(owner, os))
	require.NoError(t, err)
	assert.True(t, gone)
}

// TestObjectEngine_StaleManagedFieldMigration verifies that boxcutter can
// reconcile a new revision when the actual object has a stale "Update"
// managed field entry for the same field manager alongside the normal "Apply"
// entry. This situation can occur when the post-Create CSA migration fails for
// any reason, but in particular if it races with another controller (e.g. CA
// bundle injection on a CRD).
func TestObjectEngine_StaleManagedFieldMigration(t *testing.T) {
	comp := machinery.NewComparator(DiscoveryClient, Scheme, fieldOwner)
	oe := machinery.NewObjectEngine(
		Scheme, Client, Client, comp, fieldOwner, systemPrefix, "", nil,
	)

	ctx := t.Context()

	configMap := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "oe-test-migrate",
				"namespace": "default",
			},
			"data": map[string]any{
				"key": "value",
			},
		},
	}
	cleanupOnSuccess(t, configMap)

	// Delete any leftover from a previous failed run.
	require.NoError(t, client.IgnoreNotFound(Client.Delete(ctx, configMap.DeepCopy())))

	// Step 1: Create the object with revision 1.
	res, err := oe.Reconcile(ctx, 1, configMap)
	require.NoError(t, err)
	assert.Equal(t, machinery.ActionCreated, res.Action())

	// Step 2: Inject a stale "Update" managed field entry for our field owner.
	// This simulates what happens when the post-Create CSA migration fails due
	// to a concurrent modification.
	cmPatch := configMap.DeepCopy()
	err = Client.Patch(ctx, cmPatch,
		client.RawPatch(machinerytypes.MergePatchType,
			[]byte(`{"data":{"injected":"stale"}}`)),
		client.FieldOwner(fieldOwner))
	require.NoError(t, err)

	// Verify the stale Update entry was created alongside the Apply entry.
	hasUpdateEntry := false

	for _, mf := range cmPatch.GetManagedFields() {
		if mf.Manager == fieldOwner && mf.Operation == metav1.ManagedFieldsOperationUpdate {
			hasUpdateEntry = true

			break
		}
	}

	require.True(t, hasUpdateEntry, "expected a stale Update managed field entry for %q after MergePatch", fieldOwner)

	// Step 3: Reconcile with revision 2. The revision annotation changes from
	// "1" to "2", triggering the ctrlSituationIsController modified path which
	// does an SSA Apply without ForceOwnership. Without the fix, this fails
	// with "Apply failed with 1 conflict" because the stale Update entry owns
	// the revision annotation field.
	res, err = oe.Reconcile(ctx, 2, configMap)
	require.NoError(t, err, "reconcile with new revision should succeed after migrating stale Update managed field entry")
	assert.Equal(t, machinery.ActionUpdated, res.Action())

	// Verify the stale Update managed field entry was migrated away.
	actual := configMap.DeepCopy()
	require.NoError(t, Client.Get(ctx, client.ObjectKeyFromObject(configMap), actual))

	for _, mf := range actual.GetManagedFields() {
		if mf.Manager == fieldOwner {
			assert.Equal(t, metav1.ManagedFieldsOperationApply, mf.Operation,
				"expected only Apply entries for field owner %q, found %s", fieldOwner, mf.Operation)
		}
	}
}
