package validation

import (
	"fmt"
	"strings"

	"pkg.package-operator.run/boxcutter/machinery/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Violation is a generic violation message without
// detailed context information.
type Violation interface {
	// Message returns a single string describung the error.
	Message() string
	// Messages returns list of all individual error messages.
	Messages() []string
	// Empty returns true when no violation has been recorded.
	Empty() bool
	// String returns a human readable report of all violation messages
	// and the context the error was encountered in, if available.
	String() string
}

// PhaseViolation holds more information about violations
// happening within the context of a phase.
type PhaseViolation interface {
	Violation
	// PhaseName of the phase this violation was encountered.
	PhaseName() string
	// Objects returns object violations within the phase.
	Objects() []ObjectViolation
}

// ObjectViolation holds more information about violations
// happening with a specific object.
// May be encountered in context of a phase.
type ObjectViolation interface {
	Violation
	// ObjectRef triggering the violation.
	ObjectRef() types.ObjectRef
}

// RevisionViolation holds information on revision violations.
type RevisionViolation interface {
	Violation
	// Phases returns all violations from individual phases.
	Phases() []PhaseViolation
}

type baseViolation struct {
	msgs []string
}

// Message returns a single string describung the error.
func (v baseViolation) Message() string {
	return strings.Join(v.msgs, ", ")
}

// Messages returns list of all individual error messages.
func (v baseViolation) Messages() []string {
	return v.msgs
}

// Empty returns true when no violation has been recorded.
func (v baseViolation) Empty() bool {
	return len(v.msgs) == 0
}

// String returns a human readable report of all violation messages
// and the context the error was encountered in, if available.
func (v baseViolation) String() string {
	var out string
	for _, msg := range v.msgs {
		out += fmt.Sprintf("- %s\n", msg)
	}
	return out
}

type objectViolation struct {
	baseViolation
	obj types.ObjectRef
}

// ObjectRef triggering the violation.
func (v objectViolation) ObjectRef() types.ObjectRef {
	return v.obj
}

// String returns a human readable report of all violation messages
// and the context the error was encountered in, if available.
func (v objectViolation) String() string {
	out := v.obj.String() + ":\n"
	return out + v.baseViolation.String()
}

func newObjectViolation(obj client.Object, msgs []string) *objectViolation {
	return &objectViolation{
		baseViolation: baseViolation{msgs: msgs},
		obj:           types.ToObjectRef(obj),
	}
}

func newObjectViolationFromRef(obj types.ObjectRef, msgs []string) *objectViolation {
	return &objectViolation{
		baseViolation: baseViolation{msgs: msgs},
		obj:           obj,
	}
}

type phaseViolation struct {
	baseViolation
	phaseName string
	objects   []ObjectViolation
}

// PhaseName of the phase this violation was encountered.
func (v phaseViolation) PhaseName() string {
	return v.phaseName
}

// Objects returns object violations within the phase.
func (v phaseViolation) Objects() []ObjectViolation {
	return v.objects
}

// Empty returns true when no violation has been recorded.
func (v phaseViolation) Empty() bool {
	return len(v.msgs) == 0 && len(v.objects) == 0
}

// String returns a human readable report of all violation messages
// and the context the error was encountered in, if available.
func (v phaseViolation) String() string {
	out := fmt.Sprintf("Phase %q:\n", v.phaseName)
	out += v.baseViolation.String()
	for _, o := range v.objects {
		out += o.String()
	}
	return out
}

func newPhaseViolation(
	phaseName string, msgs []string,
	objects []ObjectViolation,
) *phaseViolation {
	return &phaseViolation{
		baseViolation: baseViolation{msgs: msgs},
		phaseName:     phaseName,
		objects:       objects,
	}
}

type revisionViolation struct {
	baseViolation
	phases []PhaseViolation
}

// Phases returns all violations from individual phases.
func (v revisionViolation) Phases() []PhaseViolation {
	return v.phases
}

// String returns a human readable report of all violation messages
// and the context the error was encountered in, if available.
func (v revisionViolation) String() string {
	var out string
	for _, o := range v.phases {
		out += o.String()
	}
	return out
}

func newRevisionViolation(
	msgs []string,
	phases []PhaseViolation,
) *revisionViolation {
	return &revisionViolation{
		baseViolation: baseViolation{msgs: msgs},
		phases:        phases,
	}
}
