package managedcache

import (
	"context"
	"sync"

	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type cacheSettings struct {
	source     *cacheSource
	handler    handler.EventHandler
	predicates []predicate.Predicate
}

// For printing in startup log messages.
func (e cacheSettings) String() string { return "managedcache.CacheSource" }

var _ source.Source = (*cacheSettings)(nil)

type eventHandler struct {
	ctx        context.Context
	queue      workqueue.TypedRateLimitingInterface[reconcile.Request]
	handler    handler.EventHandler
	predicates []predicate.Predicate
}

// Implements source.Source interface to be used as event source when setting up controllers.
func (e cacheSettings) Start(ctx context.Context, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) error {
	e.source.mu.Lock()
	defer e.source.mu.Unlock()

	if e.source.blockNew {
		panic("Trying to add EventHandlers to managedcache.CacheSource after manager start")
	}

	e.source.handlers = append(e.source.handlers, eventHandler{ctx, queue, e.handler, e.predicates})

	return nil
}

type cacheSource struct {
	mu       sync.RWMutex
	handlers []eventHandler
	blockNew bool
	settings []cacheSettings
}

func (e *cacheSource) Source(handler handler.EventHandler, predicates ...predicate.Predicate) source.Source {
	if handler == nil {
		panic("handler is nil")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.blockNew {
		panic("Trying to add EventHandlers to managedcache.CacheSource after manager start")
	}

	s := cacheSettings{e, handler, predicates}
	e.settings = append(e.settings, s)

	return s
}

// Disables registration of new EventHandlers.
func (e *cacheSource) blockNewRegistrations() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.blockNew = true
}

// Adds all registered EventHandlers to the given informer.
func (e *cacheSource) handleNewInformer(informer cache.Informer) error {
	// this read lock should not be needed,
	// because the cacheSource should block registration
	// of new event handlers at this point
	e.mu.RLock()
	defer e.mu.RUnlock()

	// ensure to add all event handlers to the new informer
	for _, eh := range e.handlers {
		s := source.Informer{Informer: informer, Handler: eh.handler, Predicates: eh.predicates}
		if err := s.Start(eh.ctx, eh.queue); err != nil {
			return err
		}
	}

	return nil
}
