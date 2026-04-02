//go:build integration

package boxcutter

import (
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"pkg.package-operator.run/boxcutter"
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

	re := newTestRevisionEngineBuilder().build(t)
	res, err := re.Reconcile(ctx, boxcutter.NewRevisionWithOwner(
		"test-collision-prevention-prevent-unowned-cm", 1,
		[]boxcutter.Phase{
			boxcutter.NewPhase(
				"simple",
				[]client.Object{
					toUns(colliding),
				},
			),
		},
		owner, ownerhandling.NewNative(Scheme),
	))
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

	re := newTestRevisionEngineBuilder().build(t)
	res, err := re.Reconcile(ctx, boxcutter.NewRevisionWithOwner(
		"test-collision-prevention-prevent-owned-cm", 1,
		[]boxcutter.Phase{
			boxcutter.NewPhase(
				"simple",
				[]client.Object{
					toUns(colliding),
				},
			),
		},
		owner, ownerhandling.NewNative(Scheme),
	))
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
			ConflictingOwner() (*metav1.OwnerReference, bool)
		}

		hco, ok := object.(hasConflictingOwner)
		require.True(t, ok)
		conflictingOwner, ok := hco.ConflictingOwner()
		require.True(t, ok)
		assert.Equal(t, existingOwner.GetUID(), conflictingOwner.UID)
	}
}

// TestCollisionProtectionNotInCache exercises collision protection when the
// object exists on the cluster but is not in the (label-filtered) cache.
func TestCollisionProtectionNotInCache(t *testing.T) {
	tests := []struct {
		name                 string
		collisionProtection  types.CollisionProtection
		withUnfilteredReader bool
		withExistingOwner    bool // whether the pre-existing object has a different controller owner

		expectAdopted        bool // true = object should be adopted and updated
		expectCollisionError bool // true = Reconcile returns CreateCollisionError
	}{
		{
			name:                 "IfNoController adopts unowned object",
			collisionProtection:  boxcutter.CollisionProtectionIfNoController,
			withUnfilteredReader: true,
			expectAdopted:        true,
		},
		{
			name:                 "Prevent blocks unowned object",
			collisionProtection:  boxcutter.CollisionProtectionPrevent,
			withUnfilteredReader: true,
			expectAdopted:        false,
		},
		{
			name:                 "None adopts owned object",
			collisionProtection:  boxcutter.CollisionProtectionNone,
			withUnfilteredReader: true,
			withExistingOwner:    true,
			expectAdopted:        true,
		},
		{
			name:                 "without UnfilteredReader returns CreateCollisionError",
			collisionProtection:  boxcutter.CollisionProtectionIfNoController,
			withUnfilteredReader: false,
			expectCollisionError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := logr.NewContext(t.Context(), testr.New(t))
			prefix := "test-notincache-" + strings.ReplaceAll(strings.ToLower(tc.name), " ", "-") + "-"

			// Pre-create the object without boxcutter labels so it's
			// invisible to the label-filtered cache.
			existing := newConfigMap(prefix+"cm", map[string]string{
				"banana": "bread",
			})

			if tc.withExistingOwner {
				existingOwner := newConfigMap(prefix+"existing-owner", map[string]string{})
				require.NoError(t, Client.Create(ctx, existingOwner))
				cleanupOnSuccess(t, existingOwner)

				require.NoError(t, controllerutil.SetControllerReference(existingOwner, existing, Scheme))
			}

			require.NoError(t, Client.Create(ctx, existing))
			cleanupOnSuccess(t, existing)

			owner := newConfigMap(prefix+"owner", map[string]string{})
			require.NoError(t, Client.Create(ctx, owner))
			cleanupOnSuccess(t, owner)

			desired := newConfigMap(prefix+"cm", map[string]string{
				"banana": "bread",
				"apple":  "pie",
			})

			reBuilder := newTestRevisionEngineBuilder().withFilteredCache()
			if tc.withUnfilteredReader {
				reBuilder = reBuilder.withUnfilteredReader()
			}

			re := reBuilder.build(t)

			res, err := re.Reconcile(ctx, boxcutter.NewRevisionWithOwner(
				prefix+"rev", 1,
				[]boxcutter.Phase{
					boxcutter.NewPhase(
						"simple",
						[]client.Object{toUns(desired)},
					).WithReconcileOptions(
						boxcutter.WithCollisionProtection(tc.collisionProtection),
					),
				},
				owner, ownerhandling.NewNative(Scheme),
			))

			if tc.expectCollisionError {
				require.Error(t, err)

				var collisionErr *machinery.CreateCollisionError
				require.ErrorAs(t, err, &collisionErr, "should be a CreateCollisionError")
			} else {
				require.NoError(t, err)
			}

			actual := &corev1.ConfigMap{}
			require.NoError(t, Client.Get(ctx, client.ObjectKeyFromObject(existing), actual))

			if tc.expectAdopted {
				assert.True(t, res.IsComplete(), "revision should be complete after adoption")
				assert.Equal(t, "pie", actual.Data["apple"], "adopted object should have new data")
			} else {
				assert.Equal(t, existing.Data, actual.Data, "object should not be modified")

				if !tc.expectCollisionError {
					// When there's no error, verify the result reports collision.
					phases := res.GetPhases()
					require.Len(t, phases, 1)
					objects := phases[0].GetObjects()
					require.Len(t, objects, 1)
					assert.Equal(t, machinery.ActionCollision, objects[0].Action())
				}
			}
		})
	}
}
