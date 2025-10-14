package probing

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ConditionProbe checks if the object's condition is set and in a certain status.
type ConditionProbe struct {
	Type, Status string
}

var _ Prober = (*ConditionProbe)(nil)

// Probe executes the probe.
func (cp *ConditionProbe) Probe(obj client.Object) ProbeResult {
	return probeUnstructuredSingleMsg(obj, cp.probe)
}

func (cp *ConditionProbe) probe(obj *unstructured.Unstructured) ProbeResult {
	rawConditions, exist, err := unstructured.NestedFieldNoCopy(
		obj.Object, "status", "conditions")
	conditions, ok := rawConditions.([]any)

	if err != nil || !exist {
		return ProbeResult{
			Status:   ProbeStatusUnknown,
			Messages: []string{"missing .status.conditions"},
		}
	}

	if !ok {
		return ProbeResult{
			Status:   ProbeStatusUnknown,
			Messages: []string{"malformed .status.conditions"},
		}
	}

	for _, condI := range conditions {
		cond, ok := condI.(map[string]any)
		if !ok {
			// no idea what this is supposed to be
			return ProbeResult{
				Status:   ProbeStatusUnknown,
				Messages: []string{"malformed .status.conditions"},
			}
		}

		if cond["type"] != cp.Type {
			// not the type we are probing for
			continue
		}

		// Check the condition's observed generation, if set
		if observedGeneration, ok, err := unstructured.NestedInt64(
			cond, "observedGeneration",
		); err == nil && ok && observedGeneration != obj.GetGeneration() {
			return ProbeResult{
				Status:   ProbeStatusUnknown,
				Messages: []string{fmt.Sprintf(`.status.condition["%s"] outdated`, cp.Type)},
			}
		}

		if cond["status"] == cp.Status {
			return ProbeResult{
				Status:   ProbeStatusTrue,
				Messages: []string{fmt.Sprintf(`.status.condition["%s"] is %s`, cp.Type, cp.Status)},
			}
		}

		return ProbeResult{
			Status:   ProbeStatusFalse,
			Messages: []string{fmt.Sprintf(`.status.condition["%s"] is %s`, cp.Type, cond["status"])},
		}
	}

	return ProbeResult{
		Status:   ProbeStatusUnknown,
		Messages: []string{fmt.Sprintf(`missing .status.condition["%s"]`, cp.Type)},
	}
}
