package managedcache

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// ObjectBoundAccessManager manages caches and clients bound to objects.
// Each object instance will receive it's own cache and client instance.
type ObjectBoundAccessManager[T refType] interface {
	manager.Runnable
	// Get returns a TrackingCache for the provided object if one exists.
	// If one does not exist, a new Cache is created and returned.
	Get(context.Context, T) (Accessor, error)

	// GetWithUser returns a TrackingCache for the provided object if one exist.
	// If one does not exist, a new Cache is created and returned.
	// The additional user and usedFor parameters are used to automatically
	// stop informers for objects that are no longer watched.
	// After all users have called .FreeWithUser(), the cache itself will be stopped.
	GetWithUser(
		ctx context.Context, owner T,
		user client.Object, usedFor []client.Object,
	) (Accessor, error)

	// Free will stop and remove a TrackingCache for
	// the provided object, if one exists.
	Free(context.Context, T) error

	// FreeWithUser informs the manager that the given user no longer needs
	// a cache scoped to owner T. If the cache has no active users, it will be stopped.
	FreeWithUser(ctx context.Context, owner T, user client.Object) error

	// Source returns a controller-runtime source to watch from a controller.
	Source(handler.EventHandler, ...predicate.Predicate) source.Source
}

// Accessor provides write and cached read access to the cluster.
type Accessor interface {
	client.Writer
	TrackingCache
}

// NewObjectBoundAccessManager returns a new ObjectBoundAccessManager for T.
func NewObjectBoundAccessManager[T refType](
	log logr.Logger,
	mapConfig ConfigMapperFunc[T],
	baseRestConfig *rest.Config,
	baseCacheOptions cache.Options,
) ObjectBoundAccessManager[T] {
	return &objectBoundAccessManagerImpl[T]{
		log:              log.WithName("ObjectBoundAccessManager"),
		scheme:           baseCacheOptions.Scheme,
		restMapper:       baseCacheOptions.Mapper,
		mapConfig:        mapConfig,
		baseRestConfig:   baseRestConfig,
		baseCacheOptions: baseCacheOptions,

		cacheSourcer: &cacheSource{},
		newClient:    client.New,

		accessors:         map[types.UID]accessorEntry{},
		accessorRequestCh: make(chan accessorRequest[T]),
		accessorStopCh:    make(chan accessorRequest[T]),
	}
}

type refType interface {
	client.Object
	comparable
}

// ConfigMapperFunc applies changes to rest.Config and cache.Options based on the given object.
type ConfigMapperFunc[T refType] func(
	context.Context, T, *rest.Config, cache.Options) (*rest.Config, cache.Options, error)

type newClientFunc func(config *rest.Config, opts client.Options) (client.Client, error)

var _ ObjectBoundAccessManager[client.Object] = (*objectBoundAccessManagerImpl[client.Object])(nil)

type objectBoundAccessManagerImpl[T refType] struct {
	log              logr.Logger
	scheme           *runtime.Scheme
	restMapper       meta.RESTMapper
	mapConfig        ConfigMapperFunc[T]
	baseRestConfig   *rest.Config
	baseCacheOptions cache.Options

	cacheSourcer cacheSourcer
	newClient    newClientFunc

	accessors         map[types.UID]accessorEntry
	accessorRequestCh chan accessorRequest[T]
	accessorStopCh    chan accessorRequest[T]
}

type accessorEntry struct {
	accessor Accessor
	users    map[types.UID]sets.Set[schema.GroupVersionKind]
	cancel   func()
}

type accessorRequest[T refType] struct {
	owner      T
	user       client.Object
	gvks       sets.Set[schema.GroupVersionKind]
	responseCh chan<- accessorResponse
}

type accessorResponse struct {
	cache Accessor
	err   error
}

type cacheDone struct {
	err error
	uid types.UID
}

// implements Accessor interface.
type accessor struct {
	TrackingCache
	client.Writer
}

func (m *objectBoundAccessManagerImpl[T]) Source(
	handler handler.EventHandler, predicates ...predicate.Predicate,
) source.Source {
	return m.cacheSourcer.Source(handler, predicates...)
}

func (m *objectBoundAccessManagerImpl[T]) Start(ctx context.Context) error {
	ctx = logr.NewContext(ctx, m.log)

	var wg sync.WaitGroup
	doneCh := make(chan cacheDone)
	defer close(doneCh)

	for {
		select {
		case done := <-doneCh:
			// Remove accessor from list.
			delete(m.accessors, done.uid)
			if done.err != nil && !errors.Is(done.err, context.Canceled) {
				return fmt.Errorf("cache for UID %s crashed: %w", done.uid, done.err)
			}

		case req := <-m.accessorRequestCh:
			cache, err := m.handleAccessorRequest(ctx, req, doneCh, &wg)
			req.responseCh <- accessorResponse{
				cache: cache,
				err:   err,
			}

		case req := <-m.accessorStopCh:
			req.responseCh <- accessorResponse{
				err: m.handleAccessorStop(ctx, req),
			}

		case <-ctx.Done():
			// Drain doneCh to ensure shutdown does not block.
			go func() {
				//nolint:revive
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

func (m *objectBoundAccessManagerImpl[T]) handleAccessorStop(
	ctx context.Context, req accessorRequest[T],
) error {
	cache, ok := m.accessors[req.owner.GetUID()]
	if !ok {
		// nothing todo.
		return nil
	}

	if req.user != nil {
		delete(cache.users, req.owner.GetUID())
	}

	return m.gcCache(ctx, req.owner)
}

func (m *objectBoundAccessManagerImpl[T]) gcCache(ctx context.Context, owner T) error {
	log := logr.FromContextOrDiscard(ctx)

	entry, ok := m.accessors[owner.GetUID()]
	if !ok {
		return nil
	}

	if len(entry.users) == 0 {
		// no users left -> close
		log.Info("no users left, closing cache")
		entry.cancel()
		delete(m.accessors, owner.GetUID())
		return nil
	}

	inUseGVKs := sets.Set[schema.GroupVersionKind]{}
	for _, gvks := range entry.users {
		inUseGVKs.Insert(gvks.UnsortedList()...)
	}
	return entry.accessor.RemoveOtherInformers(ctx, inUseGVKs.UnsortedList()...)
}

func (m *objectBoundAccessManagerImpl[T]) handleAccessorRequest(
	ctx context.Context, req accessorRequest[T],
	doneCh chan<- cacheDone, wg *sync.WaitGroup,
) (Accessor, error) {
	log := logr.FromContextOrDiscard(ctx)
	log = log.WithValues(
		"ownerUID", req.owner.GetUID(),
	)
	ctx = logr.NewContext(ctx, log)

	entry, ok := m.accessors[req.owner.GetUID()]
	if ok {
		log.V(-1).Info("reusing cache for owner")
		if req.user != nil {
			entry.users[req.owner.GetUID()] = req.gvks
		}
		return entry.accessor, m.gcCache(ctx, req.owner)
	}

	restConfig, cacheOpts, err := m.mapConfig(
		ctx, req.owner, rest.CopyConfig(m.baseRestConfig), m.baseCacheOptions)
	if err != nil {
		return nil, fmt.Errorf("mapping rest.Config and cache.Options: %w", err)
	}

	ctrlcache, err := newTrackingCache(m.log, m.cacheSourcer, restConfig, cacheOpts)
	if err != nil {
		return nil, fmt.Errorf("creating new Cache: %w", err)
	}

	client, err := m.newClient(restConfig, client.Options{
		Scheme:     m.baseCacheOptions.Scheme,
		Mapper:     m.baseCacheOptions.Mapper,
		HTTPClient: m.baseCacheOptions.HTTPClient,
	})
	if err != nil {
		return nil, fmt.Errorf("creating new Client: %w", err)
	}

	// start cache
	ctx, cancel := context.WithCancel(ctx)
	a := &accessor{
		TrackingCache: ctrlcache,
		Writer:        client,
	}
	entry = accessorEntry{
		accessor: a,
		users:    map[types.UID]sets.Set[schema.GroupVersionKind]{},
		cancel:   cancel,
	}
	if req.user != nil {
		entry.users[req.user.GetUID()] = req.gvks
		log = log.WithValues(
			"userUID", req.user.GetUID(),
			"usedForGVKs", req.gvks.UnsortedList(),
		)
	}
	m.accessors[req.owner.GetUID()] = entry
	log.V(-1).Info("starting new cache")
	wg.Add(1)
	go func(ctx context.Context, doneCh chan<- cacheDone) {
		defer wg.Done()
		doneCh <- cacheDone{uid: req.owner.GetUID(), err: ctrlcache.Start(ctx)}
	}(ctx, doneCh)
	return a, nil
}

// request handles internal requests to start or stop an accessor.
func (m *objectBoundAccessManagerImpl[T]) request(
	ctx context.Context, accCh chan accessorRequest[T], req accessorRequest[T],
) (Accessor, error) {
	var zeroT T
	if req.owner == zeroT {
		panic("nil owner provided")
	}
	if len(req.owner.GetUID()) == 0 {
		panic("owner without UID set")
	}

	if req.user != nil {
		if len(req.user.GetUID()) == 0 {
			panic("user without UID set")
		}
	}

	responseCh := make(chan accessorResponse, 1)
	req.responseCh = responseCh
	select {
	case accCh <- req:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	// read response
	select {
	case resp := <-responseCh:
		return resp.cache, resp.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *objectBoundAccessManagerImpl[T]) Get(ctx context.Context, owner T) (Accessor, error) {
	return m.GetWithUser(ctx, owner, nil, nil)
}

func (m *objectBoundAccessManagerImpl[T]) GetWithUser(
	ctx context.Context, owner T,
	user client.Object, usedFor []client.Object,
) (Accessor, error) {
	gvks := sets.Set[schema.GroupVersionKind]{}
	for _, obj := range usedFor {
		gvk, err := apiutil.GVKForObject(obj, m.scheme)
		if err != nil {
			return nil, err
		}
		gvks.Insert(gvk)
	}

	req := accessorRequest[T]{
		owner: owner,
		user:  user,
		gvks:  gvks,
	}
	return m.request(ctx, m.accessorRequestCh, req)
}

func (m *objectBoundAccessManagerImpl[T]) Free(ctx context.Context, owner T) error {
	return m.FreeWithUser(ctx, owner, nil)
}

func (m *objectBoundAccessManagerImpl[T]) FreeWithUser(ctx context.Context, owner T, user client.Object) error {
	req := accessorRequest[T]{
		owner: owner,
		user:  user,
	}
	_, err := m.request(ctx, m.accessorStopCh, req)
	return err
}
