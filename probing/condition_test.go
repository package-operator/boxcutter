package probing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestCondition(t *testing.T) {
	t.Parallel()

	c := &ConditionProbe{
		Type:   "Available",
		Status: "False",
	}

	tests := []struct {
		name     string
		obj      *unstructured.Unstructured
		status   ProbeStatus
		messages []string
	}{
		{
			name: "succeeds",
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"status": map[string]any{
						"conditions": []any{
							map[string]any{
								"type":               "Banana",
								"status":             "True",
								"observedGeneration": int64(1), // up to date
							},
							map[string]any{
								"type":               "Available",
								"status":             "False",
								"observedGeneration": int64(1), // up to date
							},
						},
					},
					"metadata": map[string]any{
						"generation": int64(1),
					},
				},
			},
			status: ProbeStatusTrue,
			messages: []string{
				`.status.condition["Available"] is False`,
			},
		},
		{
			name: "outdated",
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"status": map[string]any{
						"conditions": []any{
							map[string]any{
								"type":               "Available",
								"status":             "False",
								"observedGeneration": int64(1), // outdated
							},
						},
					},
					"metadata": map[string]any{
						"generation": int64(42),
					},
				},
			},
			status:   ProbeStatusUnknown,
			messages: []string{`.status.condition["Available"] outdated`},
		},
		{
			name: "wrong status",
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"status": map[string]any{
						"conditions": []any{
							map[string]any{
								"type":               "Available",
								"status":             "Unknown",
								"observedGeneration": int64(1), // up to date
							},
						},
					},
					"metadata": map[string]any{
						"generation": int64(1),
					},
				},
			},
			status:   ProbeStatusFalse,
			messages: []string{`.status.condition["Available"] is Unknown`},
		},
		{
			name: "not reported",
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"status": map[string]any{
						"conditions": []any{
							map[string]any{
								"type":               "Banana",
								"status":             "True",
								"observedGeneration": int64(1), // up to date
							},
						},
					},
					"metadata": map[string]any{
						"generation": int64(1),
					},
				},
			},
			status:   ProbeStatusUnknown,
			messages: []string{`missing .status.condition["Available"]`},
		},
		{
			name: "malformed condition type int",
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"status": map[string]any{
						"conditions": []any{
							42, 56,
						},
					},
					"metadata": map[string]any{
						"generation": int64(1),
					},
				},
			},
			status:   ProbeStatusUnknown,
			messages: []string{`malformed .status.conditions`},
		},
		{
			name: "malformed condition type string",
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"status": map[string]any{
						"conditions": []any{
							"42", "56",
						},
					},
					"metadata": map[string]any{
						"generation": int64(1),
					},
				},
			},
			status:   ProbeStatusUnknown,
			messages: []string{`malformed .status.conditions`},
		},
		{
			name: "malformed conditions array",
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"status": map[string]any{
						"conditions": 42,
					},
					"metadata": map[string]any{
						"generation": int64(1),
					},
				},
			},
			status:   ProbeStatusUnknown,
			messages: []string{`malformed .status.conditions`},
		},
		{
			name: "missing conditions",
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"status": map[string]any{},
					"metadata": map[string]any{
						"generation": int64(1),
					},
				},
			},
			status:   ProbeStatusUnknown,
			messages: []string{`missing .status.conditions`},
		},
		{
			name: "missing status",
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"metadata": map[string]any{
						"generation": int64(1),
					},
				},
			},
			status:   ProbeStatusUnknown,
			messages: []string{`missing .status.conditions`},
		},
	}

	for i := range tests {
		test := tests[i]

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			r := c.Probe(test.obj)
			assert.Equal(t, test.status, r.Status)
			assert.Equal(t, test.messages, r.Messages)
		})
	}
}
