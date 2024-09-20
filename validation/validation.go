package validation

import (
	"fmt"
	"slices"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ErrorList combines multiple validation Errors.
//
//nolint:errname
type ErrorList struct {
	Errors []Error
}

func (e *ErrorList) Error() string {
	phaseErrors := map[string][]Error{}
	for _, e := range e.Errors {
		phaseErrors[e.Phase] = append(phaseErrors[e.Phase], e)
	}

	if len(phaseErrors[""]) > 0 {
		panic(fmt.Sprintf("error should be in context of a phase: %v", phaseErrors[""]))
	}

	phases := make([]string, len(phaseErrors))
	var i int
	for phase := range phaseErrors {
		phases[i] = phase
		i++
	}
	slices.Sort(phases)

	var msg string
	for _, phase := range phases {
		msg += fmt.Sprintf("Phase %q:\n", phase)
		for _, e := range phaseErrors[phase] {
			msg += strings.Join(violationsToStrings(e.Violations), "\n")
		}
	}
	return msg
}

// Error wraps a list of Violations into the error interface.
type Error struct {
	Phase      string
	Violations []Violation
}

func (e *Error) Error() string {
	var msg string
	if len(e.Phase) > 0 {
		msg = fmt.Sprintf("Phase %q:\n", e.Phase)
	}
	msg += strings.Join(violationsToStrings(e.Violations), "\n")
	return msg
}

func violationsToStrings(violations []Violation) []string {
	vs := make([]string, len(violations))
	for i, v := range violations {
		vs[i] = v.String()
	}
	return vs
}

// Violation represents a validation error.
type Violation struct {
	// Position the violation was found.
	Position string
	// Error describing the violation.
	Error string
}

func (v *Violation) String() string {
	if len(v.Position) == 0 {
		return v.Error
	}
	return fmt.Sprintf("%s: %s", v.Position, v.Error)
}

func addPositionToViolations(
	obj client.Object, vs *[]Violation,
) {
	objPosition := fmt.Sprintf("%s %s",
		obj.GetObjectKind().GroupVersionKind().Kind,
		client.ObjectKeyFromObject(obj))

	for i := range *vs {
		(*vs)[i].Position = objPosition
	}
}
