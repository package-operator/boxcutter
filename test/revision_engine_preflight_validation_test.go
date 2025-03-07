package boxcutter

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"pkg.package-operator.run/boxcutter/machinery/types"
)

func TestWithOwnerReference(t *testing.T) {
	for _, controller := range []bool{true, false} {
		name := "NoController"
		if controller {
			name = "Controller"
		}

		t.Run(name, func(t *testing.T) {
			ctx := logr.NewContext(context.Background(), testr.New(t))

			owner := newConfigMap("test-preflight-validation-owner-reference-owner", map[string]string{})
			require.NoError(t, Client.Create(ctx, owner))
			cleanupOnSuccess(ctx, t, owner)

			invalid := newConfigMap("test-preflight-validation-owner-reference-cm", map[string]string{
				"banana": "bread",
			})
			invalid.OwnerReferences = []metav1.OwnerReference{
				{
					UID:                "a",
					Kind:               "notus",
					Name:               "notuse",
					APIVersion:         "3",
					BlockOwnerDeletion: ptr.To(true),
					Controller:         ptr.To(controller),
				},
			}

			re := newTestRevisionEngine()
			res, err := re.Reconcile(ctx, types.Revision{
				Name:     "test-collision-prevention-invalid-set",
				Revision: 1,
				Owner:    owner,
				Phases: []types.Phase{
					{
						Name: "simple",
						Objects: []unstructured.Unstructured{
							toUns(invalid),
						},
					},
				},
			})
			require.NoError(t, err)
			assert.False(t, res.IsComplete())
			assert.False(t, res.InTransistion())
			require.Len(t, res.GetPhases(), 1)

			phaseViolation, ok := res.GetPhases()[0].GetPreflightViolation()
			require.True(t, ok)
			// What is it with GetPreflightViolation().Messages() being empty?
			assert.False(t, phaseViolation.Empty())
			assert.Equal(t, "simple", phaseViolation.PhaseName())
			assert.Len(t, phaseViolation.Objects(), 1)
			objectViolation := phaseViolation.Objects()[0]
			assert.False(t, objectViolation.Empty())
			assert.Equal(t, types.ObjectRef{
				GroupVersionKind: mustGVKForObject(invalid),
				ObjectKey:        client.ObjectKeyFromObject(invalid),
			}, objectViolation.ObjectRef())
			require.Len(t, objectViolation.Messages(), 1)
			assert.Equal(t, "metadata.ownerReferences: Forbidden: must be empty", objectViolation.Messages()[0])
		})
	}
}
