//go:build integration

package boxcutter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"pkg.package-operator.run/boxcutter"
	"pkg.package-operator.run/boxcutter/machinery"
	"pkg.package-operator.run/boxcutter/machinery/types"
)

type revisionEngine interface {
	Teardown(ctx context.Context, rev types.Revision, opts ...types.RevisionTeardownOption) (machinery.RevisionTeardownResult, error)
	Reconcile(ctx context.Context, rev types.Revision, opts ...types.RevisionReconcileOption) (machinery.RevisionResult, error)
}

type revisionEngineBuilder struct {
	unfilteredReader bool
	filteredCache    bool
}

func newTestRevisionEngineBuilder() revisionEngineBuilder {
	return revisionEngineBuilder{}
}

func (b revisionEngineBuilder) withUnfilteredReader() revisionEngineBuilder {
	b.unfilteredReader = true

	return b
}

func (b revisionEngineBuilder) withFilteredCache() revisionEngineBuilder {
	b.filteredCache = true

	return b
}

func (b revisionEngineBuilder) build(t *testing.T) revisionEngine {
	t.Helper()

	var (
		reader           client.Reader
		unfilteredReader client.Reader
	)

	if b.filteredCache {
		filteredCache, err := cache.New(Config, cache.Options{
			Scheme: Scheme,
			DefaultLabelSelector: labels.SelectorFromSet(labels.Set{
				"app.kubernetes.io/managed-by": "boxcutter.test",
			}),
		})
		require.NoError(t, err)

		go func() {
			_ = filteredCache.Start(t.Context())
		}()

		// Wait for the cache to start
		require.True(t, filteredCache.WaitForCacheSync(t.Context()))

		reader = filteredCache
	} else {
		reader = Client
	}

	if b.unfilteredReader {
		unfilteredReader = Client
	}

	re, err := boxcutter.NewRevisionEngine(boxcutter.RevisionEngineOptions{
		Scheme:           Scheme,
		FieldOwner:       "boxcutter.test",
		SystemPrefix:     "boxcutter.test",
		DiscoveryClient:  DiscoveryClient,
		RestMapper:       Client.RESTMapper(),
		Writer:           Client,
		Reader:           reader,
		UnfilteredReader: unfilteredReader,
	})
	must(err)

	return re
}
