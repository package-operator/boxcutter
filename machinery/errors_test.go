package machinery

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsTeardownRejectedDueToOwnerOrRevisionChange(t *testing.T) {
	t.Parallel()
	assert.True(
		t, IsTeardownRejectedDueToOwnerOrRevisionChange(TeardownRevisionError{}),
		"TeardownRevisionError is true",
	)

	assert.True(
		t, IsTeardownRejectedDueToOwnerOrRevisionChange(TeardownControllerChangedError{}),
		"TeardownControllerChangedError is true",
	)

	assert.False(
		t, IsTeardownRejectedDueToOwnerOrRevisionChange(os.ErrClosed),
		"fmt.Errorf is false",
	)
}
