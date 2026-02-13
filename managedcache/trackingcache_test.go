package managedcache

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	errSome      = errors.New("boom")
	errAPIStatus = &apierrors.StatusError{
		ErrStatus: metav1.Status{
			Message: "kaputt",
			Details: &metav1.StatusDetails{
				Group: "",
				Kind:  "configmaps",
			},
		},
	}
)

func TestTrackingCache_createTeardownInformers(t *testing.T) {
	t.Parallel()
	log := testr.New(t)
	cacheMock := &cacheMock{}
	restMapperMock := &restMapperMock{}

	tc, err := newTrackingCache(
		log, newCacheSource(),
		func(_ *rest.Config, _ cache.Options) (cache.Cache, error) {
			return cacheMock, nil
		},
		nil, cache.Options{
			Mapper: restMapperMock,
			Scheme: scheme.Scheme,
		},
	)
	require.NoError(t, err)

	itc := tc.(*trackingCache)

	informerMock := &informerMock{}

	// Mocks
	cacheMock.
		On("GetInformer", mock.Anything, mock.Anything, mock.Anything).
		Return(informerMock, nil)
	informerMock.
		On("HasSynced").
		Return(false).Once()
	informerMock.
		On("HasSynced").
		Return(true)
	cacheMock.
		On("Start", mock.Anything).
		Run(func(args mock.Arguments) {
			ctx := args.Get(0).(context.Context)
			<-ctx.Done()
		}).
		Return(nil)
	cacheMock.
		On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil)
	cacheMock.
		On("RemoveInformer", mock.Anything, mock.Anything).
		Return(nil)

	var doneWG sync.WaitGroup

	doneWG.Add(1)

	ctx, cancel := context.WithCancel(t.Context())

	go func() {
		defer doneWG.Done()

		err := tc.Start(ctx)
		if err != nil {
			panic(err)
		}
	}()

	cmObj := &corev1.ConfigMap{}
	err = itc.Get(t.Context(), client.ObjectKey{
		Name: "banana",
	}, cmObj)
	require.NoError(t, err)

	assert.Equal(t, []schema.GroupVersionKind{
		{Kind: "ConfigMap", Version: "v1"},
	}, itc.GetGVKs())

	err = itc.RemoveInformer(t.Context(), cmObj)
	require.NoError(t, err)

	informerMock.AssertExpectations(t)
	restMapperMock.AssertExpectations(t)
	cacheMock.AssertExpectations(t)

	cancel()
	doneWG.Wait()
}

func TestTrackingCache_RemoveOtherInformers(t *testing.T) {
	t.Parallel()
	log := testr.New(t)
	cacheMock := &cacheMock{}
	restMapperMock := &restMapperMock{}

	tc, err := newTrackingCache(
		log, newCacheSource(),
		func(_ *rest.Config, _ cache.Options) (cache.Cache, error) {
			return cacheMock, nil
		},
		nil, cache.Options{
			Mapper: restMapperMock,
			Scheme: scheme.Scheme,
		},
	)
	require.NoError(t, err)

	itc := tc.(*trackingCache)

	informerMock := &informerMock{}

	// Mocks
	cacheMock.
		On("GetInformer", mock.Anything, mock.Anything, mock.Anything).
		Return(informerMock, nil)
	informerMock.
		On("HasSynced").
		Return(false).Once()
	informerMock.
		On("HasSynced").
		Return(true)
	cacheMock.
		On("Start", mock.Anything).
		Run(func(args mock.Arguments) {
			ctx := args.Get(0).(context.Context)
			<-ctx.Done()
		}).
		Return(nil)
	cacheMock.
		On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil)
	cacheMock.
		On("RemoveInformer", mock.Anything, mock.Anything).
		Return(nil)

	var doneWG sync.WaitGroup

	doneWG.Add(1)

	ctx, cancel := context.WithCancel(t.Context())

	go func() {
		defer doneWG.Done()

		err := tc.Start(ctx)
		if err != nil {
			panic(err)
		}
	}()

	cmObj := &corev1.ConfigMap{}
	err = itc.Get(t.Context(), client.ObjectKey{
		Name: "banana",
	}, cmObj)
	require.NoError(t, err)

	err = itc.RemoveOtherInformers(t.Context(), sets.Set[schema.GroupVersionKind]{})
	require.NoError(t, err)

	informerMock.AssertExpectations(t)
	restMapperMock.AssertExpectations(t)
	cacheMock.AssertExpectations(t)

	cancel()
	doneWG.Wait()
}

func TestTrackingCache_WatchFree(t *testing.T) {
	t.Parallel()
	log := testr.New(t)
	cacheMock := &cacheMock{}
	restMapperMock := &restMapperMock{}

	tc, err := newTrackingCache(
		log, newCacheSource(),
		func(_ *rest.Config, _ cache.Options) (cache.Cache, error) {
			return cacheMock, nil
		},
		nil, cache.Options{
			Mapper: restMapperMock,
			Scheme: scheme.Scheme,
		},
	)
	require.NoError(t, err)

	itc := tc.(*trackingCache)

	informerMock := &informerMock{}

	// Mocks
	cacheMock.
		On("GetInformer", mock.Anything, mock.Anything, mock.Anything).
		Return(informerMock, nil)
	informerMock.
		On("HasSynced").
		Return(false).Once()
	informerMock.
		On("HasSynced").
		Return(true)
	cacheMock.
		On("Start", mock.Anything).
		Run(func(args mock.Arguments) {
			ctx := args.Get(0).(context.Context)
			<-ctx.Done()
		}).
		Return(nil)
	cacheMock.
		On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil)
	cacheMock.
		On("RemoveInformer", mock.Anything, mock.Anything).
		Return(nil)

	var doneWG sync.WaitGroup

	doneWG.Add(1)

	ctx, cancel := context.WithCancel(t.Context())

	go func() {
		defer doneWG.Done()

		err := tc.Start(ctx)
		if err != nil {
			panic(err)
		}
	}()

	user := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "user",
			Namespace: "test",
		},
	}

	err = itc.Watch(t.Context(), user, sets.New(schema.GroupVersionKind{
		Version: "v1", Kind: "ConfigMap",
	}))
	require.NoError(t, err)

	cmObj := &corev1.ConfigMap{}
	err = itc.Get(t.Context(), client.ObjectKey{
		Name: "banana",
	}, cmObj)
	require.NoError(t, err)

	err = itc.Free(t.Context(), user)
	require.NoError(t, err)

	informerMock.AssertExpectations(t)
	restMapperMock.AssertExpectations(t)
	cacheMock.AssertExpectations(t)

	cancel()
	doneWG.Wait()
}

func TestTrackingCache_handleCacheWatchError(t *testing.T) {
	t.Parallel()

	bananaGVK := schema.GroupVersionKind{
		Group:   "test",
		Kind:    "banana",
		Version: "v1",
	}
	cacheSyncInFlight := make(chan struct{}, 1)

	tests := []struct {
		name    string
		setup   func(t *testing.T, tc *trackingCache)
		err     error
		asserts func(t *testing.T, tc *trackingCache)
	}{
		{
			name: "random error",
			err:  errSome,
		},
		{
			name: "no APIStatus Details",
			err:  &apierrors.StatusError{},
		},
		{
			name: "GVKs shutting down cache waits in flight",
			setup: func(_ *testing.T, tc *trackingCache) {
				tc.waitingForSync[bananaGVK] = []chan error{}
				tc.cacheWaitInFlight[bananaGVK] = cacheSyncInFlight
			},
			err: &apierrors.StatusError{
				ErrStatus: metav1.Status{
					Details: &metav1.StatusDetails{
						Group: "test",
						Kind:  "bananas",
					},
				},
			},
			asserts: func(t *testing.T, tc *trackingCache) {
				t.Helper()
				assert.Empty(t, tc.waitingForSync)
				assert.Empty(t, tc.cacheWaitInFlight)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			log := testr.New(t)
			cacheMock := &cacheMock{}
			restMapperMock := &restMapperMock{}
			restMapperMock.
				On("KindsFor", mock.Anything).
				Return([]schema.GroupVersionKind{bananaGVK}, nil)

			cacheMock.
				On("RemoveInformer", mock.Anything, mock.Anything).
				Return(nil)

			tc, err := newTrackingCache(
				log, newCacheSource(),
				func(_ *rest.Config, _ cache.Options) (cache.Cache, error) {
					return cacheMock, nil
				},
				nil, cache.Options{
					Mapper: restMapperMock,
					Scheme: scheme.Scheme,
				},
			)
			require.NoError(t, err)

			itc := tc.(*trackingCache)

			if test.setup != nil {
				test.setup(t, itc)
			}

			err = itc.handleCacheWatchError(t.Context(), test.err)
			require.NoError(t, err)

			if test.asserts != nil {
				test.asserts(t, itc)
			}
		})
	}
}

func TestTrackingCacheWatchErrorHandling_Get(t *testing.T) {
	t.Parallel()
	log := testr.New(t)
	cacheMock := &cacheMock{}
	restMapperMock := &restMapperMock{}
	reflectorWatchErrorHandlerMock := &reflectorWatchErrorHandlerMock{}

	var wrappedErrorHandler func(ctx context.Context, r *toolscache.Reflector, err error)

	tc, err := newTrackingCache(
		log, newCacheSource(),
		func(_ *rest.Config, opts cache.Options) (cache.Cache, error) {
			wrappedErrorHandler = opts.DefaultWatchErrorHandler

			return cacheMock, nil
		},
		nil, cache.Options{
			Mapper:                   restMapperMock,
			Scheme:                   scheme.Scheme,
			DefaultWatchErrorHandler: reflectorWatchErrorHandlerMock.ErrorHandler,
		},
	)
	require.NoError(t, err)

	itc := tc.(*trackingCache)

	informerMock := &informerMock{}

	cacheMock.
		On("GetInformer", mock.Anything, mock.Anything, mock.Anything).
		Return(informerMock, nil)
	informerMock.
		On("HasSynced").
		Run(func(_ mock.Arguments) {
			go wrappedErrorHandler(t.Context(), &toolscache.Reflector{}, errAPIStatus)
		}).
		Return(false)
	cacheMock.
		On("Start", mock.Anything).
		Run(func(args mock.Arguments) {
			ctx := args.Get(0).(context.Context)
			<-ctx.Done()
		}).
		Return(nil)
	cacheMock.
		On("RemoveInformer", mock.Anything, mock.Anything).
		Return(nil)

	reflectorWatchErrorHandlerMock.
		On("ErrorHandler", mock.Anything, mock.Anything, mock.Anything).
		Return()
	restMapperMock.
		On("KindsFor", mock.Anything).
		Return([]schema.GroupVersionKind{
			{Version: "v1", Kind: "ConfigMap"},
		}, nil)

	ctx, cancel := context.WithCancel(t.Context())

	var doneWG sync.WaitGroup

	doneWG.Add(1)

	go func() {
		err := tc.Start(ctx)
		if err != nil {
			panic(err)
		}

		doneWG.Done()
	}()

	cmObj := &corev1.ConfigMap{}
	err = itc.Get(t.Context(), client.ObjectKey{
		Name: "banana",
	}, cmObj)
	require.ErrorIs(t, err, errAPIStatus)

	reflectorWatchErrorHandlerMock.AssertCalled(
		t, "ErrorHandler", mock.Anything, mock.Anything, mock.Anything)

	informerMock.AssertExpectations(t)
	restMapperMock.AssertExpectations(t)
	cacheMock.AssertExpectations(t)

	cancel()
	doneWG.Wait()
}

func TestTrackingCacheWatchErrorHandling_List(t *testing.T) {
	t.Parallel()

	log := testr.New(t)
	cacheMock := &cacheMock{}
	restMapperMock := &restMapperMock{}
	reflectorWatchErrorHandlerMock := &reflectorWatchErrorHandlerMock{}

	var wrappedErrorHandler func(ctx context.Context, r *toolscache.Reflector, err error)

	tc, err := newTrackingCache(
		log, newCacheSource(),
		func(_ *rest.Config, opts cache.Options) (cache.Cache, error) {
			wrappedErrorHandler = opts.DefaultWatchErrorHandler

			return cacheMock, nil
		},
		nil, cache.Options{
			Mapper:                   restMapperMock,
			Scheme:                   scheme.Scheme,
			DefaultWatchErrorHandler: reflectorWatchErrorHandlerMock.ErrorHandler,
		},
	)
	require.NoError(t, err)

	itc := tc.(*trackingCache)

	informerMock := &informerMock{}

	cacheMock.
		On("GetInformer", mock.Anything, mock.Anything, mock.Anything).
		Return(informerMock, nil)
	informerMock.
		On("HasSynced").
		Run(func(_ mock.Arguments) {
			go wrappedErrorHandler(t.Context(), &toolscache.Reflector{}, errAPIStatus)
		}).
		Return(false)
	cacheMock.
		On("Start", mock.Anything).
		Run(func(args mock.Arguments) {
			ctx := args.Get(0).(context.Context)
			<-ctx.Done()
		}).
		Return(nil)
	cacheMock.
		On("RemoveInformer", mock.Anything, mock.Anything).
		Return(nil)
	reflectorWatchErrorHandlerMock.
		On("ErrorHandler", mock.Anything, mock.Anything, mock.Anything).
		Return()
	restMapperMock.
		On("KindsFor", mock.Anything).
		Return([]schema.GroupVersionKind{
			{Version: "v1", Kind: "ConfigMap"},
		}, nil)

	ctx, cancel := context.WithCancel(t.Context())

	var doneWG sync.WaitGroup

	doneWG.Add(1)

	go func() {
		err := tc.Start(ctx)
		if err != nil {
			panic(err)
		}

		doneWG.Done()
	}()

	cmObj := &corev1.ConfigMapList{}
	err = itc.List(t.Context(), cmObj)
	require.ErrorIs(t, err, errAPIStatus)

	reflectorWatchErrorHandlerMock.AssertCalled(
		t, "ErrorHandler", mock.Anything, mock.Anything, mock.Anything)

	cancel()
	doneWG.Wait()
}

func TestTrackingCache_GetObjectsPerInformer(t *testing.T) {
	t.Parallel()
	log := testr.New(t)
	cacheMock := &cacheMock{}
	restMapperMock := &restMapperMock{}

	tc, err := newTrackingCache(
		log, newCacheSource(),
		func(_ *rest.Config, _ cache.Options) (cache.Cache, error) {
			return cacheMock, nil
		},
		nil, cache.Options{
			Mapper: restMapperMock,
			Scheme: scheme.Scheme,
		},
	)
	require.NoError(t, err)

	itc := tc.(*trackingCache)

	informerMock := &informerMock{}

	// Mocks
	cacheMock.
		On("GetInformer", mock.Anything, mock.Anything, mock.Anything).
		Return(informerMock, nil)
	informerMock.
		On("HasSynced").
		Return(true)
	informerMock.
		On("IsStopped").
		Return(false)
	cacheMock.
		On("Start", mock.Anything).
		Run(func(args mock.Arguments) {
			ctx := args.Get(0).(context.Context)
			<-ctx.Done()
		}).
		Return(nil)
	cacheMock.
		On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil)
	cacheMock.
		On("List", mock.Anything, mock.AnythingOfType("*unstructured.UnstructuredList"), mock.Anything).
		Run(func(args mock.Arguments) {
			list := args.Get(1).(*unstructured.UnstructuredList)
			switch list.GetKind() {
			case "ConfigMapList":
				list.Items = []unstructured.Unstructured{{}} // Single "ConfigMap"
			case "SecretList":
				list.Items = []unstructured.Unstructured{{}, {}} // Two "Secrets"
			default:
				t.Fatalf("unexpected list kind: %s", list.GetKind())
			}
		}).
		Return(nil)

	ctx, cancel := context.WithCancel(t.Context())

	var doneWG sync.WaitGroup

	doneWG.Add(1)

	go func() {
		err := tc.Start(ctx)
		if err != nil {
			panic(err)
		}

		doneWG.Done()
	}()

	// No informers
	objectsPerInformer, err := itc.GetObjectsPerInformer(t.Context())
	require.NoError(t, err)
	assert.Empty(t, objectsPerInformer)

	cmObj := &corev1.ConfigMap{}
	err = itc.Get(t.Context(), client.ObjectKey{
		Name: "banana",
	}, cmObj)
	require.NoError(t, err)

	// Expect a ConfigMap informer with a single object to be present
	objectsPerInformer, err = itc.GetObjectsPerInformer(t.Context())
	require.NoError(t, err)
	assert.Len(t, objectsPerInformer, 1)
	objects, ok := objectsPerInformer[schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}]
	require.True(t, ok)
	assert.Equal(t, 1, objects)

	secretObj := &corev1.Secret{}
	err = itc.Get(t.Context(), client.ObjectKey{
		Name: "bread",
	}, secretObj)
	require.NoError(t, err)

	cancel()
	doneWG.Wait()

	// Expect a new Secret informer with two objects to be present
	objectsPerInformer, err = itc.GetObjectsPerInformer(t.Context())
	require.NoError(t, err)
	assert.Len(t, objectsPerInformer, 2)
	objects, ok = objectsPerInformer[schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}]
	require.True(t, ok)
	assert.Equal(t, 1, objects)
	objects, ok = objectsPerInformer[schema.GroupVersionKind{Version: "v1", Kind: "Secret"}]
	require.True(t, ok)
	assert.Equal(t, 2, objects)

	informerMock.AssertExpectations(t)
	restMapperMock.AssertExpectations(t)
	cacheMock.AssertExpectations(t)
}

type reflectorWatchErrorHandlerMock struct {
	mock.Mock
}

func (m *reflectorWatchErrorHandlerMock) ErrorHandler(ctx context.Context, r *toolscache.Reflector, err error) {
	m.Called(ctx, r, err)
}

var _ cache.Cache = (*cacheMock)(nil)

type cacheMock struct {
	mock.Mock
}

func (c *cacheMock) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	args := c.Called(ctx, key, obj, opts)

	return args.Error(0)
}

func (c *cacheMock) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	args := c.Called(ctx, list, opts)

	return args.Error(0)
}

func (c *cacheMock) GetInformer(ctx context.Context, obj client.Object, opts ...cache.InformerGetOption) (cache.Informer, error) {
	args := c.Called(ctx, obj, opts)
	i, _ := args.Get(0).(cache.Informer)

	return i, args.Error(1)
}

func (c *cacheMock) GetInformerForKind(ctx context.Context, gvk schema.GroupVersionKind, opts ...cache.InformerGetOption) (cache.Informer, error) {
	args := c.Called(ctx, gvk, opts)
	i, _ := args.Get(0).(cache.Informer)

	return i, args.Error(1)
}

func (c *cacheMock) RemoveInformer(ctx context.Context, obj client.Object) error {
	args := c.Called(ctx, obj)

	return args.Error(0)
}

func (c *cacheMock) Start(ctx context.Context) error {
	args := c.Called(ctx)

	return args.Error(0)
}

func (c *cacheMock) WaitForCacheSync(ctx context.Context) bool {
	args := c.Called(ctx)

	return args.Bool(0)
}

func (c *cacheMock) IndexField(ctx context.Context, obj client.Object, field string, extractValue client.IndexerFunc) error {
	args := c.Called(ctx, obj, field, extractValue)

	return args.Error(0)
}

var _ meta.RESTMapper = (*restMapperMock)(nil)

type restMapperMock struct {
	mock.Mock
}

// KindFor takes a partial resource and returns the single match.  Returns an error if there are multiple matches.
func (m *restMapperMock) KindFor(resource schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	args := m.Called(resource)

	return args.Get(0).(schema.GroupVersionKind), args.Error(1)
}

// KindsFor takes a partial resource and returns the list of potential kinds in priority order.
func (m *restMapperMock) KindsFor(resource schema.GroupVersionResource) ([]schema.GroupVersionKind, error) {
	args := m.Called(resource)

	return args.Get(0).([]schema.GroupVersionKind), args.Error(1)
}

// ResourceFor takes a partial resource and returns the single match.  Returns an error if there are multiple matches.
func (m *restMapperMock) ResourceFor(input schema.GroupVersionResource) (schema.GroupVersionResource, error) {
	args := m.Called(input)

	return args.Get(0).(schema.GroupVersionResource), args.Error(1)
}

// ResourcesFor takes a partial resource and returns the list of potential resource in priority order.
func (m *restMapperMock) ResourcesFor(input schema.GroupVersionResource) ([]schema.GroupVersionResource, error) {
	args := m.Called(input)

	return args.Get(0).([]schema.GroupVersionResource), args.Error(1)
}

// RESTMapping identifies a preferred resource mapping for the provided group kind.
func (m *restMapperMock) RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	args := m.Called(gk, versions)
	rm := args.Get(0).(*meta.RESTMapping)

	return rm, args.Error(1)
}

// RESTMappings returns all resource mappings for the provided group kind if no
// version search is provided. Otherwise identifies a preferred resource mapping for
// the provided version(s).
func (m *restMapperMock) RESTMappings(gk schema.GroupKind, versions ...string) ([]*meta.RESTMapping, error) {
	args := m.Called(gk, versions)

	return args.Get(0).([]*meta.RESTMapping), args.Error(1)
}

func (m *restMapperMock) ResourceSingularizer(resource string) (singular string, err error) {
	args := m.Called(resource)

	return args.String(0), args.Error(1)
}

var _ cache.Informer = (*informerMock)(nil)

type informerMock struct {
	mock.Mock
}

func (m *informerMock) AddEventHandler(
	handler toolscache.ResourceEventHandler,
) (toolscache.ResourceEventHandlerRegistration, error) {
	args := m.Called(handler)

	return args.Get(0).(toolscache.ResourceEventHandlerRegistration), args.Error(1)
}

func (m *informerMock) AddEventHandlerWithResyncPeriod(
	handler toolscache.ResourceEventHandler, resyncPeriod time.Duration,
) (toolscache.ResourceEventHandlerRegistration, error) {
	args := m.Called(handler, resyncPeriod)

	return args.Get(0).(toolscache.ResourceEventHandlerRegistration), args.Error(1)
}

func (m *informerMock) AddEventHandlerWithOptions(
	handler toolscache.ResourceEventHandler, options toolscache.HandlerOptions,
) (toolscache.ResourceEventHandlerRegistration, error) {
	args := m.Called(handler, options)

	return args.Get(0).(toolscache.ResourceEventHandlerRegistration), args.Error(1)
}

func (m *informerMock) RemoveEventHandler(handle toolscache.ResourceEventHandlerRegistration) error {
	args := m.Called(handle)

	return args.Error(0)
}

func (m *informerMock) AddIndexers(indexers toolscache.Indexers) error {
	args := m.Called(indexers)

	return args.Error(0)
}

func (m *informerMock) HasSynced() bool {
	args := m.Called()

	return args.Bool(0)
}

// IsStopped returns true if the informer has been stopped.
func (m *informerMock) IsStopped() bool {
	args := m.Called()

	return args.Bool(0)
}
