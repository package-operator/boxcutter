package managedcache

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCacheSource(t *testing.T) {
	t.Parallel()
	cs := &cacheSource{}
	cs.blockNewRegistrations()

	require.PanicsWithValue(t,
		"Trying to add EventHandlers to managedcache.CacheSource after manager start",
		func() {
			_ = cs.Source(&EnqueueWatchingObjects{})
		},
	)
}
