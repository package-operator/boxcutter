package ownerhandling

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRemoveUtility(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		slice    []string
		index    int
		expected []string
	}{
		{
			name:     "remove from middle",
			slice:    []string{"a", "b", "c", "d"},
			index:    1,
			expected: []string{"a", "d", "c"},
		},
		{
			name:     "remove first element",
			slice:    []string{"a", "b", "c"},
			index:    0,
			expected: []string{"c", "b"},
		},
		{
			name:     "remove last element",
			slice:    []string{"a", "b", "c"},
			index:    2,
			expected: []string{"a", "b"},
		},
		{
			name:     "remove from single element slice",
			slice:    []string{"a"},
			index:    0,
			expected: []string{},
		},
		{
			name:     "remove from two element slice - first",
			slice:    []string{"a", "b"},
			index:    0,
			expected: []string{"b"},
		},
		{
			name:     "remove from two element slice - second",
			slice:    []string{"a", "b"},
			index:    1,
			expected: []string{"a"},
		},
	}

	for i := range testCases {
		tc := testCases[i]
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := remove(tc.slice, tc.index)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestRemoveUtilityWithInts(t *testing.T) {
	t.Parallel()

	slice := []int{10, 20, 30, 40, 50}
	result := remove(slice, 2)
	expected := []int{10, 20, 50, 40}
	assert.Equal(t, expected, result)
}
