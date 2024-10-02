package validation

import (
	"context"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation"
	"pkg.package-operator.run/boxcutter/machinery/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	return NewNamespacedPhaseValidator(restMapper, writer, "")
}

// NewNamespacedPhaseValidator returns an ObjecctValidator for single-namespace deployments.
func NewNamespacedPhaseValidator(
	restMapper restMapper,
	writer client.Writer,
	namespace string,
) *PhaseValidator {
	return &PhaseValidator{
		ObjectValidator: NewNamespacedObjectValidator(restMapper, writer, namespace),
	}
}

// Phase represents a phase consisting of multiple objects.
type Phase interface {
	// GetName returns the name of the phase.
	GetName() string
	// GetObjects returns the list of objects belonging to the phase.
	GetObjects() []unstructured.Unstructured
}

// Validate runs validation of the phase and its objects.
func (v *PhaseValidator) Validate(ctx context.Context, phase Phase) (PhaseViolation, error) {
	var objects []ObjectViolation // errors of objects within.

	// Phase name.
	msgs := validatePhaseName(phase)

	// Individual objects.
	for _, obj := range phase.GetObjects() {
		vs, err := v.ObjectValidator.Validate(ctx, &obj)
		if err != nil {
			return nil, err
		}
		if !vs.Empty() {
			objects = append(objects, vs)
		}
	}

	// Duplicates.
	objects = append(objects, checkForObjectDuplicates(phase)...)
	return newPhaseViolation(phase.GetName(), msgs, compactObjectViolations(objects)), nil
}

func validatePhaseName(phase Phase) []string {
	if errMsgs := phaseNameValid(phase.GetName()); len(errMsgs) > 0 {
		return []string{
			"phase name invalid: " + strings.Join(errMsgs, " and "),
		}
	}
	return nil
}

func phaseNameValid(phaseName string) (errs []string) {
	return validation.IsDNS1035Label(phaseName)
}

func checkForObjectDuplicates(phases ...Phase) []ObjectViolation {
	// remember unique objects and the phase we found them first in.
	uniqueObjectsInPhase := map[types.ObjectRef]string{}
	conflicts := map[types.ObjectRef]map[string]struct{}{}

	for _, phase := range phases {
		for _, obj := range phase.GetObjects() {
			ref := types.ToObjectRef(&obj)
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

	ovs := make([]ObjectViolation, 0, len(conflicts))
	for objRef, phasesMap := range conflicts {
		var phases []string
		for p := range phasesMap {
			phases = append(phases, p)
		}
		slices.Sort(phases)
		ov := newObjectViolationFromRef(objRef, []string{
			"Duplicate object also found in phases " + strings.Join(phases, ", "),
		})
		ovs = append(ovs, ov)
	}
	return ovs
}

// merges ObjectViolations targeting the same object.
func compactObjectViolations(ovs []ObjectViolation) []ObjectViolation {
	uniqueOVs := map[types.ObjectRef][]string{}
	for _, ov := range ovs {
		uniqueOVs[ov.ObjectRef()] = append(
			uniqueOVs[ov.ObjectRef()], ov.Messages()...)
	}

	out := make([]ObjectViolation, 0, len(uniqueOVs))
	for oref, msgs := range uniqueOVs {
		out = append(out, newObjectViolationFromRef(oref, msgs))
	}
	return out
}
