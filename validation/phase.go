package validation

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"pkg.package-operator.run/boxcutter/machinery/types"
)

// PhaseValidator valiates a phase with all contained objects.
// Intended as a preflight check be ensure a higher success chance when
// rolling out the phase and prevent partial application of phases.
type PhaseValidator struct {
	*ObjectValidator
}

// NewClusterPhaseValidator returns an PhaseValidator for cross-cluster deployments.
func NewClusterPhaseValidator(
	restMapper restMapper,
	writer client.Writer,
) *PhaseValidator {
	return &PhaseValidator{
		ObjectValidator: NewClusterObjectValidator(restMapper, writer),
	}
}

// NewNamespacedPhaseValidator returns an ObjecctValidator for single-namespace deployments.
func NewNamespacedPhaseValidator(
	restMapper restMapper,
	writer client.Writer,
) *PhaseValidator {
	return &PhaseValidator{
		ObjectValidator: NewNamespacedObjectValidator(restMapper, writer),
	}
}

// Validate runs validation of the phase and its objects.
func (v *PhaseValidator) Validate(ctx context.Context, owner client.Object, phase types.Phase) (*PhaseError, error) {
	var objects []ObjectError // errors of objects within.

	// Phase name.
	var errs []error
	if len(phase.GetName()) > 0 {
		// TODO: This is due to ObjectSetPhases not knowing their phase name.
		// Not sure this is the best way to deal with this...
		errs = append(errs, validatePhaseName(phase))
	}

	// Individual objects.
	for _, o := range phase.GetObjects() {
		obj := &o

		vs, err := v.ObjectValidator.Validate(ctx, owner, obj)

		switch {
		case err != nil:
			return nil, err
		case vs != nil:
			objects = append(objects, *vs)
		}
	}

	// Duplicates.
	objects = append(objects, checkForObjectDuplicates(phase)...)

	return newPhaseError(phase.GetName(), errors.Join(errs...), compactObjectErrors(objects)), nil
}

func validatePhaseName(phase types.Phase) error {
	if errMsgs := validation.IsDNS1035Label(phase.GetName()); len(errMsgs) > 0 {
		return fmt.Errorf("phase name invalid: %v", strings.Join(errMsgs, " and "))
	}

	return nil
}

func checkForObjectDuplicates(phases ...types.Phase) []ObjectError {
	// remember unique objects and the phase we found them first in.
	uniqueObjectsInPhase := map[types.ObjectRef]string{}
	conflicts := map[types.ObjectRef]map[string]struct{}{}

	for _, phase := range phases {
		for _, o := range phase.GetObjects() {
			obj := &o
			ref := types.ToObjectRef(obj)

			otherPhase, ok := uniqueObjectsInPhase[ref]
			if !ok {
				continue
			}

			// Conflict!
			if _, ok := conflicts[ref]; !ok {
				conflicts[ref] = map[string]struct{}{
					otherPhase: {},
				}
			}

			conflicts[ref][phase.GetName()] = struct{}{}
		}
	}

	ovs := make([]ObjectError, 0, len(conflicts))

	for objRef, phasesMap := range conflicts {
		var phases []string
		for p := range phasesMap {
			phases = append(phases, p)
		}

		slices.Sort(phases)
		ov := newObjectErrorFromRef(
			objRef,
			fmt.Errorf("duplicate object also found in phases %s", strings.Join(phases, ", ")),
		)
		ovs = append(ovs, *ov)
	}

	return ovs
}

// merges ObjectErrors targeting the same object.
func compactObjectErrors(ovs []ObjectError) []ObjectError {
	uniqueOVs := map[types.ObjectRef][]error{}
	for _, ov := range ovs {
		uniqueOVs[ov.objectRef] = append(uniqueOVs[ov.objectRef], ov.err)
	}

	out := make([]ObjectError, 0, len(uniqueOVs))

	for oref, errs := range uniqueOVs {
		if len(errs) == 1 {
			out = append(out, *newObjectErrorFromRef(oref, errs[0]))
		} else {
			out = append(out, *newObjectErrorFromRef(oref, errors.Join(errs...)))
		}
	}

	return out
}
