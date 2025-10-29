package probing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestFieldsEqual(t *testing.T) {
	t.Parallel()

	fe := &FieldsEqualProbe{
		FieldA: ".spec.fieldA",
		FieldB: ".spec.fieldB",
	}

	tests := []struct {
		name     string
		obj      *unstructured.Unstructured
		status   Status
		messages []string
	}{
		{
			name: "simple succeeds",
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"spec": map[string]any{
						"fieldA": "test",
						"fieldB": "test",
					},
				},
			},
			status: StatusTrue,
			messages: []string{
				`".spec.fieldA" == ".spec.fieldB"`,
			},
		},
		{
			name: "simple not equal",
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"spec": map[string]any{
						"fieldA": "test",
						"fieldB": "not test",
					},
				},
			},
			status:   StatusFalse,
			messages: []string{`".spec.fieldA" != ".spec.fieldB" expected: "test" got: "not test"`},
		},
		{
			name: "complex succeeds",
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"spec": map[string]any{
						"fieldA": map[string]any{
							"fk": "fv",
						},
						"fieldB": map[string]any{
							"fk": "fv",
						},
					},
				},
			},
			status:   StatusTrue,
			messages: []string{`".spec.fieldA" == ".spec.fieldB"`},
		},
		{
			name: "maps not equal",
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"spec": map[string]any{
						"fieldA": map[string]any{
							"fk": "fv",
						},
						"fieldB": map[string]any{
							"fk": "something else",
						},
					},
				},
			},
			status:   StatusFalse,
			messages: []string{`".spec.fieldA" != ".spec.fieldB" expected: {"fk":"fv"} got: {"fk":"something else"}`},
		},
		{
			name: "int not equal",
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"spec": map[string]any{
						"fieldA": map[string]any{
							"fk": 1.0,
						},
						"fieldB": map[string]any{
							"fk": 2.0,
						},
					},
				},
			},
			status:   StatusFalse,
			messages: []string{`".spec.fieldA" != ".spec.fieldB" expected: {"fk":1} got: {"fk":2}`},
		},
		{
			name: "fieldA missing",
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"spec": map[string]any{
						"fieldB": "test",
					},
				},
			},
			status:   StatusFalse,
			messages: []string{`".spec.fieldA" missing`},
		},
		{
			name: "fieldB missing",
			obj: &unstructured.Unstructured{
				Object: map[string]any{
					"spec": map[string]any{
						"fieldA": "test",
					},
				},
			},
			status:   StatusFalse,
			messages: []string{`".spec.fieldB" missing`},
		},
	}

	for i := range tests {
		test := tests[i]

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			r := fe.Probe(test.obj)
			assert.Equal(t, test.status, r.Status)
			assert.Equal(t, test.messages, r.Messages)
		})
	}
}
