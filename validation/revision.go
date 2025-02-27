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
func (v *RevisionValidator) Validate(_ context.Context, rev types.Revision) (*RevisionError, error) {
	pvs := staticValidateMultiplePhases(rev.GetPhases()...)

	return newRevisionError(nil, pvs), nil
}

func staticValidateMultiplePhases(phases ...types.Phase) []PhaseError {
	commonViolations := checkForObjectDuplicates(phases...)
	pvs := []PhaseError{}

	for _, phase := range phases {
		phaseMsgs := validatePhaseName(phase)

		var ovs []ObjectError
		ovs = append(ovs, commonViolations...)

		for _, o := range phase.GetObjects() {
			obj := &o
			if err := validateObjectMetadata(obj); err != nil {
				ovs = append(ovs, *newObjectErrorFromObj(obj, err))
			}
		}

		if phaseMsgs != nil && len(ovs) == 0 {
			continue
		}

		pvs = append(pvs, *newPhaseError(phase.GetName(), phaseMsgs, compactObjectErrors(ovs)))
	}

	return pvs
}
