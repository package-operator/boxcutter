//go:build integration

package boxcutter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"pkg.package-operator.run/boxcutter/machinery"
	"pkg.package-operator.run/boxcutter/machinery/ownerhandling"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestObjectEngine(t *testing.T) {
	os := ownerhandling.NewNative(Scheme)
	comp := machinery.NewComparator(os, DiscoveryClient, Scheme, fieldOwner)
	oe := machinery.NewObjectEngine(
		Scheme, &noopCache{Reader: Client}, Client,
		Client, os, comp, fieldOwner, systemPrefix,
	)

	ctx := context.Background()
	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "oe-owner",
			Namespace: "default",
		},
	}
	require.NoError(t, Client.Create(ctx, owner, client.FieldOwner(fieldOwner)))
	t.Cleanup(func() {
		if err := Client.Delete(ctx, owner); err != nil {
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
Probe:  Succeeded
`, res.String())

	// Idle
	res, err = oe.Reconcile(ctx, owner, 1, configMap)
	require.NoError(t, err)
	assert.Equal(t, `Object ConfigMap.v1 default/oe-test
Action: "Idle"
Probe:  Succeeded
`, res.String())

	// Add other participant.
	err = Client.Patch(ctx,
		configMap.DeepCopy(),
		client.RawPatch(client.Apply.Type(), []byte(
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
Probe:  Succeeded
Other:
- "Franz"
  .data.test5
Comparison:
- Added:
  .data.test5
`, res.String())

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
Probe:  Succeeded
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
}

type noopCache struct {
	client.Reader
}

func (c *noopCache) Watch(
	_ context.Context, _ client.Object, _ runtime.Object,
) error {
	return nil
}
