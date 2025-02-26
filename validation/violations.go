package validation

import (
	"fmt"

	"pkg.package-operator.run/boxcutter/machinery/types"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RevisionError holds information on revision violations.
type RevisionError struct {
	err error
	// Phases returns all violations from individual phases.
	phases []PhaseError
}

func (e RevisionError) Error() string {
	out := ""

	if e.err != nil {
		out += e.err.Error()
	}

	for _, o := range e.phases {
		out += o.Error()
	}

	return out
}

func newRevisionError(err error, phaseErrs []PhaseError) *RevisionError {
	return &RevisionError{err, phaseErrs}
}

// PhaseError holds more information about violations happening within the context of a phase.
// May be encountered in context of a revision.
type PhaseError struct {
	// PhaseName of the phase this violation was encountered.
	phaseName string
	err       error
	// Objects returns object violations within the phase.
	objects []ObjectError
}

func (e PhaseError) Error() string {
	out := fmt.Sprintf("phase %q:\n", e.phaseName)

	if e.err != nil {
		out += e.err.Error()
	}

	for _, o := range e.objects {
		out += o.Error()
	}

	return out
}

func newPhaseError(name string, err error, objErrs []ObjectError) *PhaseError {
	return &PhaseError{name, err, objErrs}
}

// NewPhaseErrorWithErrs creates a new PhaseError of the phase identenfied by name,
// the given errors and no associated ObjectErrors.
func NewPhaseErrorWithErrs(name string, err error) *PhaseError {
	return &PhaseError{name, err, nil}
}

// ObjectError holds more information about violations happening with a specific object.
// May be encountered in context of a phase.
type ObjectError struct {
	// ObjectRef triggering the violation.
	objectRef types.ObjectRef

	err error
}

func (e ObjectError) Error() string {
	out := fmt.Sprintf("phase %q:\n", e.objectRef)

	if e.err != nil {
		out += e.Error()
	}

	return out
}

func newObjectErrorFromRef(ref types.ObjectRef, err error) *ObjectError {
	return &ObjectError{ref, err}
}

func newObjectErrorFromObj(obj client.Object, err error) *ObjectError {
	return &ObjectError{types.ToObjectRef(obj), err}
}
