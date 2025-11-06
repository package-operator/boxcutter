package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProbeResultContainerType(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()

		c := ProbeResultContainer{}

		r := c.Type("Test")
		assert.Equal(t, ProbeResult{
			Status:   ProbeStatusUnknown,
			Messages: []string{`no such probe "Test"`},
		}, r)
	})

	t.Run("found", func(t *testing.T) {
		t.Parallel()

		expected := ProbeResult{
			Status:   ProbeStatusTrue,
			Messages: []string{"test123"},
		}
		c := ProbeResultContainer{
			"Test": expected,
		}

		r := c.Type("Test")
		assert.Equal(t, expected, r)
	})
}
