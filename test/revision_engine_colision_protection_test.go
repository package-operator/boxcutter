//go:build integration

package boxcutter

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"pkg.package-operator.run/boxcutter/machinery"
	"pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/ownerhandling"
)

func TestCollisionProtectionPreventUnowned(t *testing.T) {
	ctx := logr.NewContext(t.Context(), testr.New(t))

	owner := newConfigMap("test-collision-prevention-prevent-unowned-cm-owner", map[string]string{})
	require.NoError(t, Client.Create(ctx, owner))
	cleanupOnSuccess(t, owner)

	existing := newConfigMap("test-collision-prevention-prevent-unowned-cm", map[string]string{
		"banana": "bread",
	})
	colliding := existing.DeepCopy()

	require.NoError(t, Client.Create(ctx, existing))
	cleanupOnSuccess(t, existing)

	colliding.Data["apple"] = "pie"

	ownerMetadata := ownerhandling.NewNativeRevisionMetadata(owner, Scheme)

	re := newTestRevisionEngine()
	res, err := re.Reconcile(ctx, types.Revision{
		Name:     "test-collision-prevention-prevent-unowned-cm",
		Revision: 1,
		Metadata: ownerMetadata,
		Phases: []types.Phase{
			{
				Name: "simple",
				Objects: []unstructured.Unstructured{
					toUns(colliding),
				},
			},
		},
	})
	require.NoError(t, err)
	assert.False(t, res.IsComplete())
	assert.True(t, res.InTransition())

	phases := res.GetPhases()
	require.Len(t, phases, 1)
	objects := phases[0].GetObjects()
	require.Len(t, objects, 1)
	assert.Equal(t, machinery.ActionCollision, objects[0].Action())

	actual := &corev1.ConfigMap{}
	require.NoError(t, Client.Get(ctx, client.ObjectKeyFromObject(existing), actual))
	assert.Equal(t, existing.Data, actual.Data)
}

func TestCollisionProtectionPreventOwned(t *testing.T) {
	ctx := logr.NewContext(t.Context(), testr.New(t))

	existingOwner := newConfigMap("test-collision-prevention-prevent-owned-owner", map[string]string{})
	require.NoError(t, Client.Create(ctx, existingOwner))
	cleanupOnSuccess(t, existingOwner)

	existing := newConfigMap("test-collision-prevention-prevent-owned-cm", map[string]string{
		"banana": "bread",
	})
	require.NoError(t, controllerutil.SetControllerReference(existingOwner, existing, Scheme))
	require.NoError(t, Client.Create(ctx, existing))
	cleanupOnSuccess(t, existing)

	colliding := newConfigMap("test-collision-prevention-prevent-owned-cm", map[string]string{
		"banana": "bread",
		"apple":  "pie",
	})

	owner := newConfigMap("test-collision-prevention-prevent-owned-cm-owner", map[string]string{})
	require.NoError(t, Client.Create(ctx, owner))
	cleanupOnSuccess(t, owner)

	ownerMetadata := ownerhandling.NewNativeRevisionMetadata(owner, Scheme)

	re := newTestRevisionEngine()
	res, err := re.Reconcile(ctx, types.Revision{
		Name:     "test-collision-prevention-prevent-owned-cm",
		Revision: 1,
		Metadata: ownerMetadata,
		Phases: []types.Phase{
			{
				Name: "simple",
				Objects: []unstructured.Unstructured{
					toUns(colliding),
				},
			},
		},
	})
	require.NoError(t, err)
	assert.False(t, res.IsComplete())
	assert.True(t, res.InTransition())
	require.Len(t, res.GetPhases(), 1)
	phase := res.GetPhases()[0]
	require.Len(t, phase.GetObjects(), 1)
	object := phase.GetObjects()[0]
	assert.Equal(t, schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	}, object.Object().GetObjectKind().GroupVersionKind())
	assert.Equal(t, machinery.ActionCollision, object.Action())
	{
		// Why does this type assertion not work even though t.Logf("%T", object)
		// logs machinery.ObjectResultCollision?
		// hco, ok := object.(*machinery.ObjectResultCollision)
		// Crude workaround:
		type hasConflictingOwner interface {
			ConflictingOwner() (types.RevisionReference, bool)
		}

		hco, ok := object.(hasConflictingOwner)
		require.True(t, ok)
		conflictingOwner, ok := hco.ConflictingOwner()
		require.True(t, ok)
		// RevisionReference is an interface - type assert to get the OwnerReference
		ownerRef, ok := conflictingOwner.(*metav1.OwnerReference)
		require.True(t, ok)
		assert.Equal(t, existingOwner.GetUID(), ownerRef.UID)
	}
}
