package probing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func Test_FieldValueProbe(t *testing.T) {
	t.Parallel()

	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{
				"field": "test",
			},
		},
	}

	for _, tc := range []struct {
		name           string
		probe          FieldValueProbe
		expectedResult Result
	}{
		{
			name: "True result with found key and equal value",
			probe: FieldValueProbe{
				FieldPath: "spec.field",
				Value:     "test",
			},
			expectedResult: Result{
				Status: StatusTrue,
				Messages: []string{
					`value at key "spec.field" == "test"`,
				},
			},
		},
		{
			name: "False result with missing key",
			probe: FieldValueProbe{
				FieldPath: "spec.foo",
				Value:     "test",
			},
			expectedResult: Result{
				Status: StatusFalse,
				Messages: []string{
					`missing key: "spec.foo"`,
				},
			},
		},
		{
			name: "False result with found key and unequal value",
			probe: FieldValueProbe{
				FieldPath: "spec.field",
				Value:     "bar",
			},
			expectedResult: Result{
				Status: StatusFalse,
				Messages: []string{
					`value at key "spec.field" != "bar"; expected: "bar" got: "test"`,
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expectedResult, tc.probe.Probe(obj))
		})
	}
}
