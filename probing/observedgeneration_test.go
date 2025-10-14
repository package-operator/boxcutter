package probing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestStatusObservedGeneration(t *testing.T) {
	t.Parallel()

	properMock := &proberMock{}
	og := &ObservedGenerationProbe{
		Prober: properMock,
	}

	properMock.
		On("Probe", mock.Anything).
		Return(ProbeResult{Status: ProbeStatusTrue, Messages: []string{"banana"}})

	tests := []struct {
		name     string
		obj      *unstructured.Unstructured
		status   ProbeStatus
		messages []string
	}{
		{
			name: "outdated",
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"metadata": map[string]any{
						"generation": int64(4),
					},
					"status": map[string]any{
						"observedGeneration": int64(2),
					},
				},
			},
			status:   ProbeStatusUnknown,
			messages: []string{".status outdated"},
		},
		{
			name: "up-to-date",
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"metadata": map[string]any{
						"generation": int64(4),
					},
					"status": map[string]any{
						"observedGeneration": int64(4),
					},
				},
			},
			status:   ProbeStatusTrue,
			messages: []string{"banana"},
		},
		{
			name: "not reported",
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"metadata": map[string]any{
						"generation": int64(4),
					},
					"status": map[string]any{},
				},
			},
			status:   ProbeStatusTrue,
			messages: []string{"banana"},
		},
	}

	for i := range tests {
		test := tests[i]

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			r := og.Probe(test.obj)
			assert.Equal(t, test.status, r.Status)
			assert.Equal(t, test.messages, r.Messages)
		})
	}
}
