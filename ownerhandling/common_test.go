package ownerhandling

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRemove(t *testing.T) {
	t.Parallel()

	type tcase struct {
		index    int
		expected []string
	}

	for _, tcase := range []tcase{
		{
			index:    0,
			expected: []string{"three", "two"},
		},
		{
			index:    1,
			expected: []string{"one", "three"},
		},
		{
			index:    2,
			expected: []string{"one", "two"},
		},
	} {
		t.Run(strconv.Itoa(tcase.index), func(t *testing.T) {
			t.Parallel()

			in := []string{"one", "two", "three"}
			out := remove(in, tcase.index)
			assert.Equal(t, tcase.expected, out)
		})
	}
}
