package probing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func Test_NewCELProbe(t *testing.T) {
	t.Parallel()

	_, err := NewCELProbe(`self.test`, "")
	require.ErrorIs(t, err, ErrCELInvalidEvaluationType)
}

func Test_celProbe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		rule     string
		messages []string
		obj      *unstructured.Unstructured

		status Status
	}{
		{
			name:     "simple success",
			rule:     `self.metadata.name == "hans"`,
			messages: []string{"aaaaaah!"},
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"metadata": map[string]any{
						"name": "hans",
					},
				},
			},
			status: StatusTrue,
		},
		{
			name:     "simple failure",
			rule:     `self.metadata.name == "hans"`,
			messages: []string{"aaaaaah!"},
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"metadata": map[string]any{
						"name": "nothans",
					},
				},
			},
			status: StatusFalse,
		},
		{
			name:     "OpenShift Route success simple",
			rule:     `self.status.ingress.all(i, i.conditions.all(c, c.type == "Ready" && c.status == "True"))`,
			messages: []string{"aaaaaah!"},
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"status": map[string]any{
						"test": []any{"1", "2", "3"},
						"ingress": []any{
							map[string]any{
								"host": "hostname.xxx.xxx",
								"conditions": []any{
									map[string]any{
										"type":   "Ready",
										"status": "True",
									},
								},
							},
						},
					},
				},
			},
			status: StatusTrue,
		},
		{
			name:     "OpenShift Route failure",
			rule:     `self.status.ingress.all(i, i.conditions.all(c, c.type == "Ready" && c.status == "True"))`,
			messages: []string{"aaaaaah!"},
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"status": map[string]any{
						"test": []any{"1", "2", "3"},
						"ingress": []any{
							map[string]any{
								"host": "hostname.xxx.xxx",
								"conditions": []any{
									map[string]any{
										"type":   "Ready",
										"status": "True",
									},
								},
							},
							map[string]any{
								"host": "otherhost.xxx.xxx",
								"conditions": []any{
									map[string]any{
										"type":   "Ready",
										"status": "False",
									},
								},
							},
						},
					},
				},
			},
			status: StatusFalse,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			p, err := NewCELProbe(test.rule, test.messages[0])
			require.NoError(t, err)

			r := p.Probe(test.obj)
			assert.Equal(t, test.status, r.Status)

			if test.status == StatusTrue {
				assert.Empty(t, r.Messages)
			} else {
				assert.Equal(t, test.messages, r.Messages)
			}
		})
	}
}
