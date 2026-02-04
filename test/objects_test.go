//go:build integration

package boxcutter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	machinerytypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"pkg.package-operator.run/boxcutter/machinery"
	"pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/ownerhandling"
)

func TestObjectEngine(t *testing.T) {
	os := ownerhandling.NewNative(Scheme)
	comp := machinery.NewComparator(os, DiscoveryClient, Scheme, fieldOwner)
	oe := machinery.NewObjectEngine(
		Scheme, Client, Client, os, comp, fieldOwner, systemPrefix,
	)

	ctx := t.Context()
	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "oe-owner",
			Namespace: "default",
		},
	}
	require.NoError(t, Client.Create(ctx, owner, client.FieldOwner(fieldOwner)))
	t.Cleanup(func() {
		//nolint:usetesting
		if err := Client.Delete(context.Background(), owner); err != nil {
			t.Error(err)
		}
	})

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "oe-test",
			Namespace: "default",
		},
		Data: map[string]string{
			"test1": "test",
			"test2": "test",
		},
	}

	// Creation
	res, err := oe.Reconcile(ctx, owner, 1, configMap)
	require.NoError(t, err)
	assert.Equal(t, `Object ConfigMap.v1 default/oe-test
Action: "Created"
`, res.String())
	assert.Equal(t, machinery.ActionCreated, res.Action())
	assert.True(t, res.IsComplete(), "IsComplete")
	assert.False(t, res.IsPaused(), "IsPaused")

	// Idle
	res, err = oe.Reconcile(ctx, owner, 1, configMap)
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
	res, err = oe.Reconcile(ctx, owner, 1, configMap)
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
	configMap.Annotations = map[string]string{
		"my-annotation": "test",
	}
	configMap.Data = map[string]string{
		"test1":    "new-value",
		"new-test": "new-value",
	}
	res, err = oe.Reconcile(ctx, owner, 1, configMap)
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
	gone, err := oe.Teardown(ctx, owner, 1, configMap)
	require.NoError(t, err)
	assert.False(t, gone)

	gone, err = oe.Teardown(ctx, owner, 1, configMap)
	require.NoError(t, err)
	assert.True(t, gone)
}

func TestObjectEnginePaused(t *testing.T) {
	os := ownerhandling.NewNative(Scheme)
	comp := machinery.NewComparator(os, DiscoveryClient, Scheme, fieldOwner)
	oe := machinery.NewObjectEngine(
		Scheme, Client, Client, os, comp, fieldOwner, systemPrefix,
	)

	ctx := t.Context()
	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "oe-owner-paused",
			Namespace: "default",
		},
	}
	require.NoError(t, Client.Create(ctx, owner, client.FieldOwner(fieldOwner)))
	t.Cleanup(func() {
		//nolint:usetesting
		if err := Client.Delete(context.Background(), owner); err != nil {
			t.Error(err)
		}
	})

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "oe-test-paused",
			Namespace: "default",
		},
		Data: map[string]string{
			"test1": "test",
			"test2": "test",
		},
	}
	originalConfigMap := configMap.DeepCopy()

	// Creation Paused
	res, err := oe.Reconcile(ctx, owner, 1, configMap, types.WithPaused{})
	require.NoError(t, err)
	assert.Equal(t, `Object ConfigMap.v1 default/oe-test-paused
Action (PAUSED): "Created"
`, res.String())
	assert.False(t, res.IsComplete(), "IsComplete")
	assert.True(t, res.IsPaused(), "IsPaused")

	cmShouldNotExist := &corev1.ConfigMap{}
	err = Client.Get(ctx, client.ObjectKeyFromObject(configMap), cmShouldNotExist)
	require.True(t, apierrors.IsNotFound(err), "Object should not exist after paused create action")

	// Creation Not Paused
	res, err = oe.Reconcile(ctx, owner, 1, configMap)
	require.NoError(t, err)
	assert.Equal(t, `Object ConfigMap.v1 default/oe-test-paused
Action: "Created"
`, res.String())
	assert.True(t, res.IsComplete(), "IsComplete")
	assert.False(t, res.IsPaused(), "IsPaused")

	// Idle Paused
	res, err = oe.Reconcile(ctx, owner, 1, configMap, types.WithPaused{})
	require.NoError(t, err)
	assert.Equal(t, `Object ConfigMap.v1 default/oe-test-paused
Action (PAUSED): "Idle"
`, res.String())
	assert.True(t, res.IsComplete(), "IsComplete")
	assert.True(t, res.IsPaused(), "IsPaused")

	// Update Paused.
	configMap.Annotations = map[string]string{
		"my-annotation": "test",
	}
	configMap.Data = map[string]string{
		"test1":    "new-value",
		"new-test": "new-value",
	}
	res, err = oe.Reconcile(ctx, owner, 1, configMap, types.WithPaused{})
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

	cmNotUpdated := &corev1.ConfigMap{}
	err = Client.Get(ctx, client.ObjectKeyFromObject(configMap), cmNotUpdated)
	require.NoError(t, err)
	assert.Equal(t, originalConfigMap.Data, cmNotUpdated.Data)
	assert.Equal(t, originalConfigMap.Annotations["my-annotation"], cmNotUpdated.Annotations["my-annotation"])

	// Teardown is a two step process at the moment.
	gone, err := oe.Teardown(ctx, owner, 1, configMap)
	require.NoError(t, err)
	assert.False(t, gone)

	gone, err = oe.Teardown(ctx, owner, 1, configMap)
	require.NoError(t, err)
	assert.True(t, gone)
}

func TestObjectEngineProbing(t *testing.T) {
	os := ownerhandling.NewNative(Scheme)
	comp := machinery.NewComparator(os, DiscoveryClient, Scheme, fieldOwner)
	oe := machinery.NewObjectEngine(
		Scheme, Client, Client, os, comp, fieldOwner, systemPrefix,
	)

	ctx := t.Context()
	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "oe-owner-probing",
			Namespace: "default",
		},
	}
	require.NoError(t, Client.Create(ctx, owner, client.FieldOwner(fieldOwner)))
	t.Cleanup(func() {
		//nolint:usetesting
		if err := Client.Delete(context.Background(), owner); err != nil {
			t.Error(err)
		}
	})

	probeSuccess := &stubProbe{status: types.ProbeStatusTrue, messages: []string{"works!"}}
	probeFailed := &stubProbe{status: types.ProbeStatusFalse, messages: []string{"does not work!"}}
	probeUnknown := &stubProbe{status: types.ProbeStatusUnknown, messages: []string{"no clue!"}}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "oe-test-probing",
			Namespace: "default",
		},
		Data: map[string]string{
			"test1": "test",
			"test2": "test",
		},
	}
	// Creation progress probe fails
	res, err := oe.Reconcile(
		ctx, owner, 1, configMap,
		types.WithProbe(types.ProgressProbeType, probeFailed),
		types.WithProbe("other", probeSuccess),
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
		ctx, owner, 1, configMap,
		types.WithProbe(types.ProgressProbeType, probeUnknown),
		types.WithProbe("other", probeSuccess),
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
		ctx, owner, 1, configMap,
		types.WithProbe(types.ProgressProbeType, probeSuccess),
		types.WithProbe("other", probeSuccess),
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
	gone, err := oe.Teardown(ctx, owner, 1, configMap)
	require.NoError(t, err)
	assert.False(t, gone)

	gone, err = oe.Teardown(ctx, owner, 1, configMap)
	require.NoError(t, err)
	assert.True(t, gone)
}
