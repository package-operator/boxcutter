package probing

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Prober check Kubernetes objects for certain conditions and report success or failure with failure messages.
// type Prober = types.Prober

// Prober needs to be implemented by any probing implementation.
type Prober interface {
	Probe(obj client.Object) ProbeResult
}

// ProbeStatus may be "True", "False" or "Unknown".
type ProbeStatus string

const (
	// ProbeStatusTrue means the probe has succeeded.
	ProbeStatusTrue ProbeStatus = "True"
	// ProbeStatusFalse means the probe has failed.
	ProbeStatusFalse ProbeStatus = "False"
	// ProbeStatusUnknown means the probe was unable to determine the state.
	ProbeStatusUnknown ProbeStatus = "Unknown"
)

// ProbeResult combines a ProbeState with human readable messages describing how the state happened.
type ProbeResult struct {
	// Status of the probe result, one of True, False, Unknown.
	Status ProbeStatus
	// Messages are human readable status descriptions containing details about the reported state.
	Messages []string
}

// And combines multiple Prober.
// The returned status is:
// - True if all ProbeResults are True
// - False if at least one ProbeResult is False and none are Unknown
// - Unknown if at least one ProbeResult is Unknown
// Messages of the same Status will be combined.
type And []Prober

var _ Prober = (And)(nil)

// Probe runs probes against the given object and returns the result.
func (p And) Probe(obj client.Object) ProbeResult {
	var unknownMsgs, trueMsgs, falseMsgs []string

	var statusUnknown, statusFalse bool

	for _, probe := range p {
		r := probe.Probe(obj)
		switch r.Status {
		case ProbeStatusTrue:
			trueMsgs = append(trueMsgs, r.Messages...)
		case ProbeStatusFalse:
			statusFalse = true

			falseMsgs = append(falseMsgs, r.Messages...)
		case ProbeStatusUnknown:
			statusUnknown = true

			unknownMsgs = append(unknownMsgs, r.Messages...)
		}
	}

	if statusUnknown {
		return ProbeResult{
			Status:   ProbeStatusUnknown,
			Messages: unknownMsgs,
		}
	}

	if statusFalse {
		return ProbeResult{
			Status:   ProbeStatusFalse,
			Messages: falseMsgs,
		}
	}

	return ProbeResult{
		Status:   ProbeStatusTrue,
		Messages: trueMsgs,
	}
}

func toUnstructured(obj client.Object) *unstructured.Unstructured {
	unstr, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		panic(fmt.Sprintf("can't convert to unstructured: %v", err))
	}

	return &unstructured.Unstructured{Object: unstr}
}

func probeUnstructuredSingleMsg(
	obj client.Object,
	probe func(obj *unstructured.Unstructured) ProbeResult,
) ProbeResult {
	unst := toUnstructured(obj)

	return probe(unst)
}
