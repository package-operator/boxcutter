//go:build integration

package boxcutter

import (
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
	"pkg.package-operator.run/boxcutter/validation"
)

func TestWithOwnerReference(t *testing.T) {
	for _, controller := range []bool{true, false} {
		name := "NoController"
		if controller {
			name = "Controller"
		}

		t.Run(name, func(t *testing.T) {
			ctx := logr.NewContext(t.Context(), testr.New(t))

			owner := newConfigMap("test-preflight-validation-owner-reference-owner", map[string]string{})
			require.NoError(t, Client.Create(ctx, owner))
			cleanupOnSuccess(t, owner)

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
			assert.False(t, res.InTransition())

			var objValErr validation.ObjectValidationError

			require.ErrorAs(t, res.GetValidationError(), &objValErr)
			assert.Equal(t, types.ObjectRef{
				GroupVersionKind: mustGVKForObject(invalid),
				ObjectKey:        client.ObjectKeyFromObject(invalid),
			}, objValErr.ObjectRef)
			require.Len(t, objValErr.Errors, 1)
			assert.Equal(t, "metadata.ownerReferences: Forbidden: must be empty", objValErr.Errors[0].Error())
		})
	}
}
