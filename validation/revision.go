package validation

import (
	"context"

	"pkg.package-operator.run/boxcutter/machinery/types"
)

// RevisionValidator runs basic validation against
// all phases and objects making up a revision.
//
// It performes less detailed checks than ObjectValidator or PhaseValidator
// as detailed checks (using e.g. dry run) should only be run right before
// a phase is installed to prevent false positives.
type RevisionValidator struct{}

// NewRevisionValidator returns a new RevisionValidator instance.
func NewRevisionValidator() *RevisionValidator {
	return &RevisionValidator{}
}

// Validate a revision compromising of multiple phases.
func (v *RevisionValidator) Validate(_ context.Context, rev types.RevisionAccessor) (RevisionViolation, error) {
	pvs := staticValidateMultiplePhases(rev.GetPhases()...)

	return newRevisionViolation(nil, pvs), nil
}

func staticValidateMultiplePhases(phases ...types.PhaseAccessor) []PhaseViolation {
	commonViolations := checkForObjectDuplicates(phases...)
	pvs := []PhaseViolation{}

	for _, phase := range phases {
		phaseMsgs := validatePhaseName(phase)

		var ovs []ObjectViolation
		ovs = append(ovs, commonViolations...)

		for _, obj := range phase.GetObjects() {
			if errs := validateObjectMetadata(obj.Object); len(errs) > 0 {
				ovs = append(ovs, newObjectViolation(obj.Object, errs))
			}
		}

		if len(phaseMsgs) == 0 && len(ovs) == 0 {
			continue
		}

		pvs = append(pvs, *newPhaseViolation(
			phase.GetName(), phaseMsgs, compactObjectViolations(ovs)))
	}

	return pvs
}
