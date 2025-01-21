package managedcache

import (
	"context"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// TrackingCache is a cache remembering what objects are being cached and
// allowing to stop caches no longer needed.
type TrackingCache interface {
	cache.Cache

	Source(handler handler.EventHandler, predicates ...predicate.Predicate) source.Source

	// StopInformersNotListed stops all informers that are not needed to watch
	// the given list of object types.
	StopInformersNotListed(ctx context.Context, objs ...client.Object) error
}

type cacheSourcer interface {
	Source(handler handler.EventHandler, predicates ...predicate.Predicate) source.Source
	blockNewRegistrations()
	handleNewInformer(cache.Informer) error
}

var _ TrackingCache = (*trackingCacheImpl)(nil)

type trackingCacheImpl struct {
	cache.Cache

	scheme       *runtime.Scheme
	gvks         sets.Set[schema.GroupVersionKind]
	gvksMux      sync.Mutex
	cacheSourcer cacheSourcer
}

// NewTrackingCache returns a new TrackingCache instance.
func NewTrackingCache(c cache.Cache, scheme *runtime.Scheme) TrackingCache {
	return newTrackingCache(c, scheme, &cacheSource{})
}

func newTrackingCache(c cache.Cache, scheme *runtime.Scheme, cacheSourcer cacheSourcer) TrackingCache {
	return &trackingCacheImpl{
		Cache:        c,
		scheme:       scheme,
		gvks:         sets.Set[schema.GroupVersionKind]{},
		cacheSourcer: cacheSourcer,
	}
}

func (c *trackingCacheImpl) Source(handler handler.EventHandler, predicates ...predicate.Predicate) source.Source {
	return c.cacheSourcer.Source(handler, predicates...)
}

func (c *trackingCacheImpl) Get(
	ctx context.Context, key client.ObjectKey,
	obj client.Object, opts ...client.GetOption,
) error {
	c.gvksMux.Lock()
	defer c.gvksMux.Unlock()
	if err := c.recordObject(ctx, obj); err != nil {
		return err
	}

	return c.Cache.Get(ctx, key, obj, opts...)
}

func (c *trackingCacheImpl) List(
	ctx context.Context, list client.ObjectList,
	opts ...client.ListOption,
) error {
	c.gvksMux.Lock()
	defer c.gvksMux.Unlock()
	if err := c.recordObjectList(ctx, list); err != nil {
		return err
	}

	return c.Cache.List(ctx, list, opts...)
}

func (c *trackingCacheImpl) GetInformer(
	ctx context.Context, obj client.Object,
	opts ...cache.InformerGetOption,
) (cache.Informer, error) {
	c.gvksMux.Lock()
	defer c.gvksMux.Unlock()
	if err := c.recordObject(ctx, obj); err != nil {
		return nil, err
	}

	return c.Cache.GetInformer(ctx, obj, opts...)
}

func (c *trackingCacheImpl) GetInformerForKind(
	ctx context.Context, gvk schema.GroupVersionKind,
	opts ...cache.InformerGetOption,
) (cache.Informer, error) {
	c.gvksMux.Lock()
	defer c.gvksMux.Unlock()
	if err := c.recordGVK(ctx, gvk); err != nil {
		return nil, err
	}

	return c.Cache.GetInformerForKind(ctx, gvk, opts...)
}

// StopInformersNotListed stops all informers that are not needed to watch
// the given list of object types.
func (c *trackingCacheImpl) StopInformersNotListed(
	ctx context.Context, objs ...client.Object,
) error {
	var existingGVKs sets.Set[schema.GroupVersionKind]
	for _, obj := range objs {
		gvk, err := apiutil.GVKForObject(obj, c.scheme)
		if err != nil {
			return err
		}
		existingGVKs.Insert(gvk)
	}

	c.gvksMux.Lock()
	defer c.gvksMux.Unlock()
	toStop := existingGVKs.Difference(c.gvks)

	for _, gvk := range toStop.UnsortedList() {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)
		if err := c.Cache.RemoveInformer(ctx, obj); err != nil {
			return err
		}
		c.gvks.Delete(gvk)
	}

	return nil
}

func (c *trackingCacheImpl) recordObject(ctx context.Context, obj client.Object) error {
	gvk, err := apiutil.GVKForObject(obj, c.scheme)
	if err != nil {
		return err
	}
	return c.recordGVK(ctx, gvk)
}

func (c *trackingCacheImpl) recordObjectList(ctx context.Context, list client.ObjectList) error {
	gvk, err := apiutil.GVKForObject(list, c.scheme)
	if err != nil {
		return err
	}
	// We need the non-list GVK, so chop off the "List" from the end of the kind.
	gvk.Kind = strings.TrimSuffix(gvk.Kind, "List")

	return c.recordGVK(ctx, gvk)
}

func (c *trackingCacheImpl) recordGVK(ctx context.Context, gvk schema.GroupVersionKind) error {
	knownInformer := c.gvks.Has(gvk)
	if !knownInformer {
		i, err := c.Cache.GetInformerForKind(ctx, gvk)
		if err != nil {
			return err
		}
		c.cacheSourcer.handleNewInformer(i)
		c.gvks.Insert(gvk)
	}

	return nil
}
