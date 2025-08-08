package managedcache

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

func TestCacheSource_Source(t *testing.T) {
	t.Parallel()
	t.Run("panics on nil handler", func(t *testing.T) {
		t.Parallel()

		cs := newCacheSource()

		require.Panics(t, func() {
			cs.Source(nil)
		}, "handler is nil")
	})

	t.Run("returns a Source that can be started", func(t *testing.T) {
		t.Parallel()

		cs := newCacheSource()
		s := cs.Source(&handler.EnqueueRequestForObject{})

		ctx := t.Context()
		err := s.Start(ctx, nil)
		require.NoError(t, err)
		assert.Len(t, cs.handlers, 1)

		assert.Equal(t, cacheStringOutput, fmt.Sprintf("%s", s))
	})

	t.Run("handles new Informers", func(t *testing.T) {
		t.Parallel()

		cs := newCacheSource()

		existing := &informerMock{}
		existing.On("IsStopped").Return(true)
		cs.informers = []cache.Informer{existing}

		require.NoError(t, cs.handleNewInformer(&informerMock{}))
		assert.Len(t, cs.informers, 1)
	})
}
