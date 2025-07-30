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

const cacheStringOutput = "managedcache.CacheSource"

type cacheSettings struct {
	source     *cacheSource
	handler    handler.EventHandler
	predicates []predicate.Predicate
}

// For printing in startup log messages.
func (e cacheSettings) String() string { return cacheStringOutput }

var _ source.Source = (*cacheSettings)(nil)

type eventHandler struct {
	ctx        context.Context
	queue      workqueue.TypedRateLimitingInterface[reconcile.Request]
	handler    handler.EventHandler
	predicates []predicate.Predicate
}

// Implements source.Source interface to be used as event source when setting up controllers.
func (e cacheSettings) Start(ctx context.Context, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) error {
	return e.source.handleNewEventHandlerStart(ctx, queue, e)
}

type cacheSource struct {
	mu        sync.Mutex
	handlers  []eventHandler
	informers []cache.Informer
	settings  []cacheSettings
}

func (e *cacheSource) Source(handler handler.EventHandler, predicates ...predicate.Predicate) source.Source {
	if handler == nil {
		panic("handler is nil")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	s := cacheSettings{e, handler, predicates}
	e.settings = append(e.settings, s)

	return s
}

func (e *cacheSource) handleNewEventHandlerStart(
	ctx context.Context, queue workqueue.TypedRateLimitingInterface[reconcile.Request],
	settings cacheSettings,
) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.handlers = append(e.handlers, eventHandler{ctx, queue, settings.handler, settings.predicates})

	// ensure to connect all informers with the new handler
	for _, i := range e.informers {
		s := source.Informer{Informer: i, Handler: settings.handler, Predicates: settings.predicates}
		if err := s.Start(ctx, queue); err != nil {
			return err
		}
	}

	return nil
}

// Adds all registered EventHandlers to the given informer.
func (e *cacheSource) handleNewInformer(informer cache.Informer) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.informers = append(e.informers, informer)

	// ensure to add all event handlers to the new informer
	for _, eh := range e.handlers {
		s := source.Informer{Informer: informer, Handler: eh.handler, Predicates: eh.predicates}
		if err := s.Start(eh.ctx, eh.queue); err != nil {
			return err
		}
	}

	return nil
}
