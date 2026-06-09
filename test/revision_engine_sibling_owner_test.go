//go:build integration

package boxcutter

import (
	"strconv"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"pkg.package-operator.run/boxcutter"
	"pkg.package-operator.run/boxcutter/machinery"
	"pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/ownerhandling"
)

func TestSiblingOwnerClassifier(t *testing.T) {
	tests := []struct {
		name             string
		siblingRevision  string // revision annotation on the pre-existing object
		desiredRevision  int64  // revision we reconcile with
		expectedAction   machinery.Action
		expectComplete   bool
		expectTransition bool
		expectAdopted    bool // if true, verify controller ref changed to current owner
	}{
		{
			name:             "sibling at higher revision reports progressed",
			siblingRevision:  "4",
			desiredRevision:  1,
			expectedAction:   machinery.ActionProgressed,
			expectComplete:   true,
			expectTransition: false,
			expectAdopted:    false,
		},
		{
			name:             "sibling at lower revision triggers adoption",
			siblingRevision:  "1",
			desiredRevision:  2,
			expectedAction:   machinery.ActionUpdated,
			expectComplete:   true,
			expectTransition: false,
			expectAdopted:    true,
		},
		{
			name:             "sibling at same revision reports collision",
			siblingRevision:  "1",
			desiredRevision:  1,
			expectedAction:   machinery.ActionCollision,
			expectComplete:   false,
			expectTransition: true,
			expectAdopted:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := logr.NewContext(t.Context(), testr.New(t))
			prefix := "test-sibling-" + strings.ReplaceAll(strings.ToLower(tc.name), " ", "-") + "-"

			siblingOwner := newConfigMap(prefix+"sibling-owner", map[string]string{})
			require.NoError(t, Client.Create(ctx, siblingOwner))
			cleanupOnSuccess(t, siblingOwner)

			currentOwner := newConfigMap(prefix+"current-owner", map[string]string{})
			require.NoError(t, Client.Create(ctx, currentOwner))
			cleanupOnSuccess(t, currentOwner)

			existing := newConfigMap(prefix+"cm", map[string]string{
				"banana": "bread",
			})
			require.NoError(t, controllerutil.SetControllerReference(siblingOwner, existing, Scheme))
			existing.Annotations = map[string]string{
				systemPrefix + "/revision": tc.siblingRevision,
			}
			existing.Labels = map[string]string{
				"app.kubernetes.io/managed-by": "boxcutter.test",
			}
			require.NoError(t, Client.Create(ctx, existing))
			cleanupOnSuccess(t, existing)

			desired := newConfigMap(prefix+"cm", map[string]string{
				"banana": "bread",
				"apple":  "pie",
			})

			re := newTestRevisionEngineBuilder().build(t)
			res, err := re.Reconcile(ctx, boxcutter.NewRevisionWithOwner(
				prefix+"rev", tc.desiredRevision,
				[]boxcutter.Phase{
					boxcutter.NewPhase(
						"simple",
						[]client.Object{toUns(desired)},
					).WithReconcileOptions(
						types.WithSiblingOwnerClassifier(func(ref metav1.OwnerReference) bool {
							return ref.UID == siblingOwner.UID
						}),
					),
				},
				currentOwner, ownerhandling.NewNative(Scheme),
			))
			require.NoError(t, err)

			assert.Equal(t, tc.expectComplete, res.IsComplete(), "IsComplete")
			assert.Equal(t, tc.expectTransition, res.InTransition(), "InTransition")

			phases := res.GetPhases()
			require.Len(t, phases, 1)
			objects := phases[0].GetObjects()
			require.Len(t, objects, 1)
			assert.Equal(t, tc.expectedAction, objects[0].Action())

			actual := &corev1.ConfigMap{}
			require.NoError(t, Client.Get(ctx, client.ObjectKeyFromObject(existing), actual))

			if tc.expectAdopted {
				assert.Equal(t, "pie", actual.Data["apple"], "adopted object should have new data")
				assert.Equal(t, tc.desiredRevision, mustParseRevision(t, actual), "revision annotation should be updated")
				assertController(t, actual, currentOwner.UID)
			} else {
				assert.Equal(t, existing.Data, actual.Data, "object should not be modified")
				assert.Equal(t, tc.siblingRevision, actual.Annotations[systemPrefix+"/revision"], "revision annotation should be unchanged")
				assertController(t, actual, siblingOwner.UID)
			}
		})
	}
}

func mustParseRevision(t *testing.T, obj *corev1.ConfigMap) int64 {
	t.Helper()

	raw := obj.Annotations[systemPrefix+"/revision"]
	require.NotEmpty(t, raw, "revision annotation missing")

	rev, err := strconv.ParseInt(raw, 10, 64)
	require.NoError(t, err)

	return rev
}

func assertController(t *testing.T, obj *corev1.ConfigMap, expectedUID apitypes.UID) {
	t.Helper()

	for _, ref := range obj.OwnerReferences {
		if ref.Controller != nil && *ref.Controller {
			assert.Equal(t, expectedUID, ref.UID, "controller UID should match expected owner")

			return
		}
	}

	t.Errorf("no controller owner reference found on object %s/%s", obj.Namespace, obj.Name)
}
