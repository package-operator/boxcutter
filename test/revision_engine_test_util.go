package boxcutter

import (
	"context"

	"pkg.package-operator.run/boxcutter"
	"pkg.package-operator.run/boxcutter/machinery"
	"pkg.package-operator.run/boxcutter/machinery/types"
)

type revisionEngine interface {
	Teardown(ctx context.Context, rev types.Revision) (machinery.RevisionTeardownResult, error)
	Reconcile(ctx context.Context, rev types.Revision, opts ...types.RevisionOption) (machinery.RevisionResult, error)
}

func newTestRevisionEngine() revisionEngine {
	re, err := boxcutter.NewRevisionEngine(boxcutter.RevisionEngineOptions{
		Scheme:          Scheme,
		FieldOwner:      "boxcutter.test",
		SystemPrefix:    "boxcutter.test",
		DiscoveryClient: DiscoveryClient,
		RestMapper:      Client.RESTMapper(),
		Writer:          Client,
		Reader:          Client,
	})
	must(err)
	return re
}
