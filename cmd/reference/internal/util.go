package internal

import (
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

func prevJSON(prevRevisions []bctypes.Revision) string {
	data := make([]unstructured.Unstructured, 0, len(prevRevisions))

	for _, rev := range prevRevisions {
		refObj := getOwnerFromRev(rev)
		ref := unstructured.Unstructured{}
		ref.SetGroupVersionKind(refObj.GetObjectKind().GroupVersionKind())
		ref.SetName(refObj.GetName())
		ref.SetNamespace(refObj.GetNamespace())
		ref.SetUID(refObj.GetUID())
		data = append(data, ref)
	}

	dataJSON, err := json.Marshal(data)
	if err != nil {
		panic(err)
	}

	return string(dataJSON)
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
