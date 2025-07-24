package managedcache

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/rest"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// TrackingCache is a cache remembering what objects are being cached and
// allowing to stop caches no longer needed.
type TrackingCache interface {
	cache.Cache

	// Source returns a source to watch from a controller.
	Source(handler handler.EventHandler, predicates ...predicate.Predicate) source.Source

	// RemoveOtherInformers stops all informers that are not needed to watch the given list of object types.
	RemoveOtherInformers(ctx context.Context, gvks sets.Set[schema.GroupVersionKind]) error

	// GetGVKs returns a list of GVKs known by this trackingCache.
	GetGVKs() []schema.GroupVersionKind
}

type cacheSourcer interface {
	Source(handler handler.EventHandler, predicates ...predicate.Predicate) source.Source
	blockNewRegistrations()
	handleNewInformer(cache.Informer) error
}

// - informing the cacheSourcer about new informers.
type trackingCache struct {
	cache.Cache
	log          logr.Logger
	restMapper   meta.RESTMapper
	cacheSourcer cacheSourcer

	// Guards against informers getting removed
	// while someone is still reading.
	accessLock sync.RWMutex

	cacheWatchErrorCh chan error
	gvkRequestCh      chan trackingCacheRequest
	informerSyncCh    chan informerSyncResponse
	knownInformers    sets.Set[schema.GroupVersionKind]
	waitingForSync    map[schema.GroupVersionKind][]chan error
	cacheWaitInFlight map[schema.GroupVersionKind]chan struct{}
}

type informerSyncResponse struct {
	gvk schema.GroupVersionKind
	err error
}

type trackingCacheRequest struct {
	do func(ctx context.Context)
}

// NewTrackingCache returns a new TrackingCache instance.
func NewTrackingCache(log logr.Logger, config *rest.Config, opts cache.Options) (TrackingCache, error) {
	return newTrackingCache(log, &cacheSource{}, config, opts)
}

func newTrackingCache(
	log logr.Logger, cacheSourcer cacheSourcer,
	config *rest.Config, opts cache.Options,
) (TrackingCache, error) {
	wehc := &trackingCache{
		log:          log.WithName("TrackingCache"),
		restMapper:   opts.Mapper,
		cacheSourcer: cacheSourcer,

		cacheWatchErrorCh: make(chan error),
		gvkRequestCh:      make(chan trackingCacheRequest),
		informerSyncCh:    make(chan informerSyncResponse),
		knownInformers:    sets.Set[schema.GroupVersionKind]{},
		waitingForSync:    map[schema.GroupVersionKind][]chan error{},
		cacheWaitInFlight: map[schema.GroupVersionKind]chan struct{}{},
	}
	errHandler := opts.DefaultWatchErrorHandler
	opts.DefaultWatchErrorHandler = func(r *toolscache.Reflector, err error) {
		if errHandler != nil {
			errHandler(r, err)
		}

		if apistatus, ok := err.(apierrors.APIStatus); ok || errors.As(err, &apistatus) {
			if apistatus.Status().Details != nil {
				wehc.cacheWatchErrorCh <- err
			}
		}
	}

	c, err := cache.New(config, opts)
	if err != nil {
		return nil, err
	}

	wehc.Cache = c

	return wehc, nil
}

// GetGVKs returns a list of GVKs known by this trackingCache.
func (c *trackingCache) GetGVKs() []schema.GroupVersionKind {
	c.accessLock.RLock()
	defer c.accessLock.RUnlock()

	return c.knownInformers.UnsortedList()
}

func (c *trackingCache) Source(handler handler.EventHandler, predicates ...predicate.Predicate) source.Source {
	return c.cacheSourcer.Source(handler, predicates...)
}

func (c *trackingCache) Start(ctx context.Context) error {
	ctx = logr.NewContext(ctx, c.log)
	c.cacheSourcer.blockNewRegistrations()

	cacheErrCh := make(chan error)
	go func() {
		cacheErrCh <- c.Cache.Start(ctx)
	}()

	for {
		select {
		case res := <-c.informerSyncCh:
			for _, errCh := range c.waitingForSync[res.gvk] {
				errCh <- res.err
			}

			delete(c.waitingForSync, res.gvk)
			delete(c.cacheWaitInFlight, res.gvk)

		case req := <-c.gvkRequestCh:
			req.do(ctx)

		case err := <-c.cacheWatchErrorCh:
			if err := c.handleCacheWatchError(err); err != nil {
				return err
			}

		case err := <-cacheErrCh:
			return err

		case <-ctx.Done():
			return nil
		}
	}
}

func (c *trackingCache) handleCacheWatchError(err error) error {
	apistatus, ok := err.(apierrors.APIStatus)
	apistatusOk := ok || errors.As(err, &apistatus)

	if !apistatusOk {
		// not a APIStatus error
		return nil
	}

	status := apistatus.Status()
	if status.Details == nil {
		// can't map error to waiting GVK.
		return nil
	}

	errorGVKs, rmerr := c.restMapper.KindsFor(schema.GroupVersionResource{
		Group:    status.Details.Group,
		Resource: status.Details.Kind,
	})
	if rmerr != nil {
		return rmerr
	}

	for _, errorGVK := range errorGVKs {
		for waitingGvk, errChs := range c.waitingForSync {
			if waitingGvk.Group == errorGVK.Group &&
				waitingGvk.Kind == errorGVK.Kind {
				for _, errCh := range errChs {
					errCh <- err
				}

				delete(c.waitingForSync, waitingGvk)
				close(c.cacheWaitInFlight[waitingGvk])
			}
		}
	}

	return nil
}

func (c *trackingCache) ensureCacheSync(ctx context.Context, obj client.Object) error {
	gvk, err := gvkForObject(obj)
	if err != nil {
		return err
	}

	if err := c.ensureCacheSyncForGVK(ctx, gvk); err != nil {
		return fmt.Errorf("ensuring cache sync for GVK: %w", err)
	}

	return nil
}

func (c *trackingCache) ensureCacheSyncList(ctx context.Context, list client.ObjectList) error {
	gvk, err := gvkForObject(list)
	if err != nil {
		return err
	}
	// We need the non-list GVK, so chop off the "List" from the end of the kind.
	gvk.Kind = strings.TrimSuffix(gvk.Kind, "List")

	if err := c.ensureCacheSyncForGVK(ctx, gvk); err != nil {
		return fmt.Errorf("ensuring cache sync for (list) GVK: %w", err)
	}

	return nil
}

func (c *trackingCache) ensureCacheSyncForGVK(ctx context.Context, gvk schema.GroupVersionKind) error {
	errCh := make(chan error, 1)
	c.gvkRequestCh <- trackingCacheRequest{
		do: func(ctx context.Context) {
			log := logr.FromContextOrDiscard(ctx).WithValues("gvk", gvk)
			if _, ok := c.waitingForSync[gvk]; ok {
				// other are already waiting on the same informer to sync.
				// -> don't start another WaitForCacheSync
				log.V(-1).Info("new call waiting for WaitForCacheSync already in flight")
				c.waitingForSync[gvk] = append(c.waitingForSync[gvk], errCh)

				return
			}

			obj := &unstructured.Unstructured{}
			obj.SetGroupVersionKind(gvk)
			i, err := c.Cache.GetInformer(ctx, obj, cache.BlockUntilSynced(false))
			if err != nil {
				errCh <- err

				return
			}

			isNewInformer := !c.knownInformers.Has(gvk)
			if isNewInformer {
				c.knownInformers.Insert(gvk)
				if err := c.cacheSourcer.handleNewInformer(i); err != nil {
					errCh <- err

					return
				}
			}

			c.waitingForSync[gvk] = append(c.waitingForSync[gvk], errCh)
			if _, ok := c.cacheWaitInFlight[gvk]; ok {
				return
			}

			stopCh := make(chan struct{})
			c.cacheWaitInFlight[gvk] = stopCh
			go func() {
				if isNewInformer {
					log.V(-1).Info("waiting for new informer to sync")
				}
				if toolscache.WaitForCacheSync(stopCh, i.HasSynced) {
					log.V(-1).Info("informer synced successfully")
					c.informerSyncCh <- informerSyncResponse{gvk: gvk, err: nil}

					return
				}
				log.V(-1).Info("wait for informer sync canceled")
				c.informerSyncCh <- informerSyncResponse{gvk: gvk, err: context.Canceled}
			}()
		},
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *trackingCache) Get(
	ctx context.Context, key client.ObjectKey,
	obj client.Object, opts ...client.GetOption,
) error {
	c.accessLock.RLock()
	defer c.accessLock.RUnlock()

	if err := c.ensureCacheSync(ctx, obj); err != nil {
		return err
	}

	err := c.Cache.Get(ctx, key, obj, opts...)
	if err != nil {
		return fmt.Errorf("getting object: %w", err)
	}

	return nil
}

func (c *trackingCache) List(
	ctx context.Context, list client.ObjectList,
	opts ...client.ListOption,
) error {
	c.accessLock.RLock()
	defer c.accessLock.RUnlock()

	if err := c.ensureCacheSyncList(ctx, list); err != nil {
		return err
	}

	return c.Cache.List(ctx, list, opts...)
}

func (c *trackingCache) GetInformer(
	ctx context.Context, obj client.Object,
	opts ...cache.InformerGetOption,
) (cache.Informer, error) {
	c.accessLock.RLock()
	defer c.accessLock.RUnlock()

	if err := c.ensureCacheSync(ctx, obj); err != nil {
		return nil, err
	}

	return c.Cache.GetInformer(ctx, obj, opts...)
}

func (c *trackingCache) GetInformerForKind(
	ctx context.Context, gvk schema.GroupVersionKind,
	opts ...cache.InformerGetOption,
) (cache.Informer, error) {
	c.accessLock.RLock()
	defer c.accessLock.RUnlock()

	if err := c.ensureCacheSyncForGVK(ctx, gvk); err != nil {
		return nil, err
	}

	return c.Cache.GetInformerForKind(ctx, gvk, opts...)
}

func (c *trackingCache) RemoveInformer(ctx context.Context, obj client.Object) error {
	c.accessLock.Lock()
	defer c.accessLock.Unlock()

	gvk, err := gvkForObject(obj)
	if err != nil {
		return err
	}

	errCh := make(chan error, 1)
	c.gvkRequestCh <- trackingCacheRequest{
		do: func(ctx context.Context) {
			log := logr.FromContextOrDiscard(ctx)
			err := c.Cache.RemoveInformer(ctx, obj)
			if err != nil {
				errCh <- err

				return
			}

			log.V(-1).Info("stopping informers", "gvk", gvk)
			close(errCh)
			close(c.cacheWaitInFlight[gvk])
			c.knownInformers.Delete(gvk)
		},
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *trackingCache) RemoveOtherInformers(ctx context.Context, gvks sets.Set[schema.GroupVersionKind]) error {
	c.accessLock.Lock()
	defer c.accessLock.Unlock()

	errCh := make(chan error, 1)
	c.gvkRequestCh <- trackingCacheRequest{
		do: func(ctx context.Context) {
			log := logr.FromContextOrDiscard(ctx)

			gvksToStop := c.knownInformers.Difference(gvks).UnsortedList()
			if len(gvksToStop) > 0 {
				log.V(-1).Info("stopping informers", "gvks", gvksToStop)
			}
			for _, gvkToStop := range gvksToStop {
				obj := &unstructured.Unstructured{}
				obj.SetGroupVersionKind(gvkToStop)
				err := c.Cache.RemoveInformer(ctx, obj)
				if err != nil {
					errCh <- err

					return
				}

				if _, ok := c.cacheWaitInFlight[gvkToStop]; ok {
					close(c.cacheWaitInFlight[gvkToStop])
					delete(c.cacheWaitInFlight, gvkToStop)
				}
				c.knownInformers.Delete(gvkToStop)
			}
			close(errCh)
		},
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
