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
	q := &RateLimitingQueue{}
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	ownerRefGetter.
		On("OwnersForGKV", schema.GroupVersionKind{
			Version: "v1",
			Kind:    "Secret",
		}).
		Return([]OwnerReference{
			{
				GroupKind: schema.GroupKind{
					Kind: "ConfigMap",
				},
				Name:      "cmtest",
				Namespace: "cmtestns",
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

func (m *ownerRefGetterMock) OwnersForGKV(gvk schema.GroupVersionKind) []OwnerReference {
	args := m.Called(gvk)
	return args.Get(0).([]OwnerReference)
}

type RateLimitingQueue struct {
	mock.Mock
}

func (q *RateLimitingQueue) Add(item reconcile.Request) {
	q.Called(item)
}

func (q *RateLimitingQueue) Len() int {
	args := q.Called()
	return args.Int(0)
}

func (q *RateLimitingQueue) Get() (item reconcile.Request, shutdown bool) {
	args := q.Called()
	return args.Get(0).(reconcile.Request), args.Bool(1)
}

func (q *RateLimitingQueue) Done(item reconcile.Request) {
	q.Called(item)
}

func (q *RateLimitingQueue) ShutDown() {
	q.Called()
}

func (q *RateLimitingQueue) ShutDownWithDrain() {
	q.Called()
}

func (q *RateLimitingQueue) ShuttingDown() bool {
	args := q.Called()
	return args.Bool(0)
}

func (q *RateLimitingQueue) AddAfter(item reconcile.Request, duration time.Duration) {
	q.Called(item, duration)
}

func (q *RateLimitingQueue) AddRateLimited(item reconcile.Request) {
	q.Called(item)
}

func (q *RateLimitingQueue) Forget(item reconcile.Request) {
	q.Called(item)
}

func (q *RateLimitingQueue) NumRequeues(item reconcile.Request) int {
	args := q.Called(item)
	return args.Int(0)
}
