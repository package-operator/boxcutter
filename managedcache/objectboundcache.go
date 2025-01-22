package managedcache

import (
	"context"
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// ObjectBoundCache manages caches for individual bound to objects.
// Each object instance will receive it's own cache instance.
type ObjectBoundCache[T refType] interface {
	manager.Runnable
	// Get returns a TrackingCache for the provided object if one exists.
	// If one does not exist, a new Cache is created and returned.
	Get(context.Context, T) (ScopedCacheClient, error)
	// Free will stop and remove a TrackingCache for
	// the provided object, if one exists.
	Free(context.Context, T) error
	Source(handler.EventHandler, ...predicate.Predicate) source.Source
}

type ScopedCacheClient interface {
	client.Writer
	TrackingCache
}

func NewObjectBoundCache[T refType](
	mapConfig ConfigMapperFunc[T],
	baseRestConfig *rest.Config,
	baseCacheOptions cache.Options,
) ObjectBoundCache[T] {
	return &objectBoundCacheImpl[T]{
		scheme:           baseCacheOptions.Scheme,
		mapConfig:        mapConfig,
		baseRestConfig:   baseRestConfig,
		baseCacheOptions: baseCacheOptions,

		cacheSourcer: &cacheSource{},
		newCache:     cache.New,

		caches:         map[types.UID]cacheEntry{},
		cacheRequestCh: make(chan cacheRequest[T]),
		cacheStopCh:    make(chan cacheRequest[T]),
	}
}

type refType interface {
	client.Object
	comparable
}

// ConfigMapperFunc applies changes to rest.Config and cache.Options based on the given object.
type ConfigMapperFunc[T refType] func(context.Context, T, *rest.Config, cache.Options) (*rest.Config, cache.Options, error)

type newCacheFunc func(config *rest.Config, opts cache.Options) (cache.Cache, error)

var _ ObjectBoundCache[client.Object] = (*objectBoundCacheImpl[client.Object])(nil)

type objectBoundCacheImpl[T refType] struct {
	scheme           *runtime.Scheme
	mapConfig        ConfigMapperFunc[T]
	baseRestConfig   *rest.Config
	baseCacheOptions cache.Options

	cacheSourcer cacheSourcer
	newCache     newCacheFunc

	caches         map[types.UID]cacheEntry
	cacheRequestCh chan cacheRequest[T]
	cacheStopCh    chan cacheRequest[T]
}

type cacheEntry struct {
	cache  ScopedCacheClient
	cancel func()
}

type cacheRequest[T refType] struct {
	owner      T
	responseCh chan<- cacheResponse
}

type cacheResponse struct {
	cache ScopedCacheClient
	err   error
}

type cacheDone struct {
	err error
	uid types.UID
}

func (c *objectBoundCacheImpl[T]) Source(handler handler.EventHandler, predicates ...predicate.Predicate) source.Source {
	return c.cacheSourcer.Source(handler, predicates...)
}

func (i *objectBoundCacheImpl[T]) Start(ctx context.Context) error {
	var wg sync.WaitGroup
	doneCh := make(chan cacheDone)
	defer close(doneCh)

	for {
		select {
		case done := <-doneCh:
			// Remove cache from list.
			delete(i.caches, done.uid)
			if done.err != nil && done.err != context.Canceled {
				return fmt.Errorf("cache for UID %s crashed: %w", done.uid, done.err)
			}

		case req := <-i.cacheRequestCh:
			cache, err := i.handleCacheRequest(ctx, req, doneCh, &wg)
			req.responseCh <- cacheResponse{
				cache: cache,
				err:   err,
			}

		case req := <-i.cacheStopCh:
			cache, ok := i.caches[req.owner.GetUID()]
			if ok {
				cache.cancel()
			}
			// ensure cache is removed from map before .Free stops blocking.
			delete(i.caches, req.owner.GetUID())
			close(req.responseCh)

		case <-ctx.Done():
			// Drain doneCh to ensure shutdown does not block.
			go func() {
				for range doneCh {
				}
			}()

			// All sub-caches should also receive this signal and start to stop.
			// So we don't have to manually cancel caches individually.
			// Just wait for all to close to ensure they have gracefully shutdown.
			wg.Wait()
			return nil
		}
	}
}

type accessor struct {
	TrackingCache
	client.Writer
}

func (i *objectBoundCacheImpl[T]) handleCacheRequest(
	ctx context.Context, req cacheRequest[T],
	doneCh chan<- cacheDone, wg *sync.WaitGroup,
) (ScopedCacheClient, error) {
	c, ok := i.caches[req.owner.GetUID()]
	if ok {
		return c.cache, nil
	}

	restConfig, cacheOpts, err := i.mapConfig(
		ctx, req.owner, rest.CopyConfig(i.baseRestConfig), i.baseCacheOptions)
	if err != nil {
		return nil, fmt.Errorf("mapping rest.Config and cache.Options: %w", err)
	}

	ctrlcache, err := i.newCache(restConfig, cacheOpts)
	if err != nil {
		return nil, fmt.Errorf("creating new Cache: %w", err)
	}

	cache := newTrackingCache(ctrlcache, i.scheme, i.cacheSourcer)
	client, err := client.New(restConfig, client.Options{
		Scheme:     i.baseCacheOptions.Scheme,
		Mapper:     i.baseCacheOptions.Mapper,
		HTTPClient: i.baseCacheOptions.HTTPClient,
	})
	if err != nil {
		return nil, fmt.Errorf("creating new Client: %w", err)
	}

	// start cache
	ctx, cancel := context.WithCancel(ctx)
	a := &accessor{TrackingCache: cache, Writer: client}
	entry := cacheEntry{
		cache:  a,
		cancel: cancel,
	}
	i.caches[req.owner.GetUID()] = entry
	wg.Add(1)
	go func(ctx context.Context, doneCh chan<- cacheDone) {
		defer wg.Done()
		doneCh <- cacheDone{uid: req.owner.GetUID(), err: cache.Start(ctx)}
	}(ctx, doneCh)
	return a, nil
}

// Get returns a Cache for the provided object reference.
// If a cache does not already exist, a new one will be created.
// If a nil object is provided this function will panic.
// If the given object has no UID set this function will panic.
func (i *objectBoundCacheImpl[T]) Get(ctx context.Context, owner T) (ScopedCacheClient, error) {
	var zeroT T
	if owner == zeroT {
		panic("nil object provided")
	}
	if len(owner.GetUID()) == 0 {
		panic("object without UID set")
	}

	respCh := make(chan cacheResponse, 1)
	req := cacheRequest[T]{
		owner:      owner,
		responseCh: respCh,
	}
	select {
	case i.cacheRequestCh <- req:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	// read response
	select {
	case resp := <-respCh:
		return resp.cache, resp.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Free stops and removes the Cache for the provided object.
func (i *objectBoundCacheImpl[T]) Free(ctx context.Context, owner T) error {
	var zeroT T
	if owner == zeroT {
		panic("nil ClusterExtension provided")
	}
	if len(owner.GetUID()) == 0 {
		panic("object without UID set")
	}

	respCh := make(chan cacheResponse, 1)
	req := cacheRequest[T]{
		owner:      owner,
		responseCh: respCh,
	}
	select {
	case i.cacheStopCh <- req:
	case <-ctx.Done():
		return ctx.Err()
	}
	// read response
	select {
	case resp := <-respCh:
		return resp.err
	case <-ctx.Done():
		return ctx.Err()
	}
}
