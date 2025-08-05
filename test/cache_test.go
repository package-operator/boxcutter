//go:build integration

package boxcutter

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	k8sapierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"pkg.package-operator.run/boxcutter/managedcache"
)

const (
	pollInterval        = 1 * time.Second
	deletionWaitTimeout = 20 * time.Second
)

// This test starts and stops caches for each owner in the `owners` slice.
// For each cache, it will start and stop multiple informers based on the objects in the `owned` slice.
func TestManagedCacheStartStop(t *testing.T) {
	log := testr.New(t)

	accessManager := managedcache.NewObjectBoundAccessManager(
		log,
		func(_ context.Context, _ client.Object, config *rest.Config, options cache.Options) (*rest.Config, cache.Options, error) {
			return config, options, nil
		},
		Config,
		cache.Options{
			Scheme: scheme.Scheme,
		},
	)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		return ignoreContextCanceled(accessManager.Start(ctx))
	})

	owners := []client.Object{
		&corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				UID: "123-456",
			},
		},
		&corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				UID: "789-012",
			},
		},
	}

	owned := []client.Object{
		&corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "owned-1",
				Namespace: "default",
			},
		},
		&corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Secret",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "owned-2",
				Namespace: "default",
			},
		},
		&corev1.Service{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Service",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "owned-3",
				Namespace: "default",
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{},
				Ports: []corev1.ServicePort{
					{
						Port: 3000,
					},
				},
			},
		},
	}

	for _, owner := range owners {
		t.Run("Owner_"+string(owner.GetUID()), func(t *testing.T) {
			ctx := t.Context()

			// First get all objects in owned,
			// then all except the last,
			// then one less, ...
			// This way multiple gvks are getting watched,
			// and then watches are removed/stopped one-by-one.
			for i := range owned {
				j := len(owned) - i
				objects := owned[:j]

				gvks := []string{}
				for _, obj := range objects {
					gvks = append(gvks, obj.GetObjectKind().GroupVersionKind().Kind)
				}

				t.Run(strings.Join(gvks, "_"), func(t *testing.T) {
					accessor, err := accessManager.GetWithUser(ctx, owner, owner, objects)
					require.NoError(t, err)

					for _, object := range objects {
						// Create object.
						require.NoError(t, accessor.Create(ctx, deepCopyClientObject(object)))

						// Run multiple get requests in parallel to validate that this works.
						eg := &errgroup.Group{}
						for range 10 {
							eg.Go(func() error {
								return wait.PollUntilContextCancel(ctx, pollInterval, true, func(ctx context.Context) (done bool, err error) {
									if err := accessor.Get(ctx, client.ObjectKeyFromObject(object), deepCopyClientObject(object)); err != nil {
										return false, err
									}

									return true, nil
								})
							})
						}

						require.NoError(t, eg.Wait())

						// Caches have been synced, Get object another time to validate that this works, too.
						require.NoError(t, accessor.Get(ctx, client.ObjectKeyFromObject(object), deepCopyClientObject(object)))

						// Delete object.
						require.NoError(t, accessor.Delete(ctx, deepCopyClientObject(object)))
						// Wait until it's gone.
						require.NoError(t, wait.PollUntilContextCancel(ctx, pollInterval, true, func(ctx context.Context) (done bool, err error) {
							if err := accessor.Get(ctx, client.ObjectKeyFromObject(object), deepCopyClientObject(object)); k8sapierrors.IsNotFound(err) {
								return true, nil
							} else if err != nil {
								return false, err
							}

							return false, nil
						}))
					}

					require.NoError(t, accessManager.FreeWithUser(ctx, owner, owner))
				})
			}

			require.NoError(t, accessManager.Free(ctx, owner))
		})
	}

	cancel()
	require.NoError(t, eg.Wait())
}

func TestManagedCacheStartStopRestart(t *testing.T) {
	log := testr.New(t)

	accessManager := managedcache.NewObjectBoundAccessManager(
		log,
		func(_ context.Context, _ client.Object, config *rest.Config, options cache.Options) (*rest.Config, cache.Options, error) {
			return config, options, nil
		},
		Config,
		cache.Options{
			Scheme: scheme.Scheme,
		},
	)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		return ignoreContextCanceled(accessManager.Start(ctx))
	})

	owner := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			UID: "123-456",
		},
	}

	ownedConfigMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "owned-1",
			Namespace: "default",
		},
	}

	for range 2 {
		accessor, err := accessManager.GetWithUser(ctx, owner, owner, []client.Object{deepCopyClientObject(ownedConfigMap)})
		require.NoError(t, err)

		require.NoError(t, accessor.Create(ctx, deepCopyClientObject(ownedConfigMap)))
		require.NoError(t, wait.PollUntilContextCancel(ctx, pollInterval, true, func(ctx context.Context) (done bool, err error) {
			if err := accessor.Get(ctx, client.ObjectKeyFromObject(ownedConfigMap), deepCopyClientObject(ownedConfigMap)); err != nil {
				return false, err
			}

			return true, nil
		}))

		require.NoError(t, accessor.Delete(ctx, deepCopyClientObject(ownedConfigMap)))

		require.NoError(t, wait.PollUntilContextCancel(ctx, pollInterval, true, func(ctx context.Context) (done bool, err error) {
			if err := accessor.Get(ctx, client.ObjectKeyFromObject(ownedConfigMap), deepCopyClientObject(ownedConfigMap)); k8sapierrors.IsNotFound(err) {
				return true, nil
			} else if err != nil {
				return false, err
			}

			return false, nil
		}))

		require.NoError(t, accessManager.FreeWithUser(ctx, owner, owner))
	}

	cancel()
	require.NoError(t, eg.Wait())
}
