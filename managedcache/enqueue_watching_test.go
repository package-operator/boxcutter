package managedcache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestEnqueueWatchingObjects(t *testing.T) {
	t.Parallel()

	ownerRefGetter := &ownerRefGetterMock{}
	q := &typedRateLimitingQueueMock[reconcile.Request]{}
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	ownerRefGetter.
		On("GetWatchersForGVK", schema.GroupVersionKind{
			Version: "v1",
			Kind:    "Secret",
		}).
		Return([]AccessManagerKey{
			{
				GroupVersionKind: schema.GroupVersionKind{
					Kind: "ConfigMap",
				},
				UID: types.UID("123"),
				ObjectKey: types.NamespacedName{
					Name:      "cmtest",
					Namespace: "cmtestns",
				},
			},
		})

	q.On("Add", reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "cmtest",
			Namespace: "cmtestns",
		},
	})

	h := NewEnqueueWatchingObjects(ownerRefGetter, &corev1.ConfigMap{}, scheme)
	h.Create(context.Background(), event.CreateEvent{
		Object: &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "testns",
			},
		},
	}, q)

	q.AssertExpectations(t)
	ownerRefGetter.AssertExpectations(t)
}

type ownerRefGetterMock struct {
	mock.Mock
}

func (m *ownerRefGetterMock) GetWatchersForGVK(gvk schema.GroupVersionKind) []AccessManagerKey {
	args := m.Called(gvk)

	return args.Get(0).([]AccessManagerKey)
}

type typedRateLimitingQueueMock[T comparable] struct {
	mock.Mock
}

func (q *typedRateLimitingQueueMock[T]) Add(item T) {
	q.Called(item)
}

func (q *typedRateLimitingQueueMock[T]) Len() int {
	args := q.Called()

	return args.Int(0)
}

func (q *typedRateLimitingQueueMock[T]) Get() (item T, shutdown bool) {
	args := q.Called()

	return args.Get(0).(T), args.Bool(1)
}

func (q *typedRateLimitingQueueMock[T]) Done(item T) {
	q.Called(item)
}

func (q *typedRateLimitingQueueMock[T]) ShutDown() {
	q.Called()
}

func (q *typedRateLimitingQueueMock[T]) ShutDownWithDrain() {
	q.Called()
}

func (q *typedRateLimitingQueueMock[T]) ShuttingDown() bool {
	args := q.Called()

	return args.Bool(0)
}

func (q *typedRateLimitingQueueMock[T]) AddAfter(item T, duration time.Duration) {
	q.Called(item, duration)
}

func (q *typedRateLimitingQueueMock[T]) AddRateLimited(item T) {
	q.Called(item)
}

func (q *typedRateLimitingQueueMock[T]) Forget(item T) {
	q.Called(item)
}

func (q *typedRateLimitingQueueMock[T]) NumRequeues(item T) int {
	args := q.Called(item)

	return args.Int(0)
}
