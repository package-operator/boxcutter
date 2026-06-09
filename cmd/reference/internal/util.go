package internal

import (
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"pkg.package-operator.run/boxcutter"
	bctypes "pkg.package-operator.run/boxcutter/machinery/types"
)

type revisionAscending []bctypes.Revision

func (a revisionAscending) Len() int      { return len(a) }
func (a revisionAscending) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a revisionAscending) Less(i, j int) bool {
	iObj := a[i]
	jObj := a[j]

	return iObj.GetRevisionNumber() < jObj.GetRevisionNumber()
}

func latestRevisionNumber(prevRevisions []bctypes.Revision) int64 {
	if len(prevRevisions) == 0 {
		return 0
	}

	return prevRevisions[len(prevRevisions)-1].GetRevisionNumber()
}

func parseRevisionNumber(raw string) (int64, error) {
	return strconv.ParseInt(raw, 10, 64)
}

func getOwner(obj client.Object) (metav1.OwnerReference, bool) {
	for _, v := range obj.GetOwnerReferences() {
		if v.Controller != nil && *v.Controller {
			return v, true
		}
	}

	return metav1.OwnerReference{}, false
}

func getOwnerFromRev(rev boxcutter.Revision) client.Object {
	var options bctypes.RevisionReconcileOptions
	for _, opt := range rev.GetReconcileOptions() {
		opt.ApplyToRevisionReconcileOptions(&options)
	}

	return options.GetOwner()
}
