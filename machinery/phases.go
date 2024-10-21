package machinery

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"pkg.package-operator.run/boxcutter/machinery/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PhaseEngine groups reconciliation of a list of objects,
// after all of them have passed preflight checks and
// performs probing after the objects have been reconciled.
type PhaseEngine struct {
	objectEngine   objectEngine
	phaseValidator phaseValidator
}

// NewPhaseEngine returns a new PhaseEngine instance.
func NewPhaseEngine(
	objectEngine objectEngine,
	phaseValidator phaseValidator,
) *PhaseEngine {
	return &PhaseEngine{
		objectEngine:   objectEngine,
		phaseValidator: phaseValidator,
	}
}

type phaseValidator interface {
	Validate(
		ctx context.Context,
		owner client.Object,
		phase validation.Phase,
	) (validation.PhaseViolation, error)
}

type objectEngine interface {
	Reconcile(
		ctx context.Context,
		owner client.Object, // Owner of the object.
		revision int64, // Revision number, must start at 1.
		desiredObject *unstructured.Unstructured,
		opts ...ObjectOption,
	) (ObjectResult, error)
	Teardown(
		ctx context.Context,
		owner client.Object, // Owner of the object.
		revision int64, // Revision number, must start at 1.
		desiredObject *unstructured.Unstructured,
	) (objectDeleted bool, err error)
}

// Phase represents a phase consisting of multiple objects.
type Phase struct {
	Name    string
	Objects []PhaseObject
}

// GetName returns the name of the phase.
func (p *Phase) GetName() string {
	return p.Name
}

// GetObjects returns the list of objects belonging to the phase.
func (p *Phase) GetObjects() []unstructured.Unstructured {
	objects := make([]unstructured.Unstructured, 0, len(p.Objects))
	for _, o := range p.Objects {
		objects = append(objects, *o.Object)
	}
	return objects
}

// PhaseObject represents an object and it's options.
type PhaseObject struct {
	Object *unstructured.Unstructured
	Opts   []ObjectOption
}

// Teardown ensures the given phase is safely removed from the cluster.
func (e *PhaseEngine) Teardown(
	ctx context.Context,
	owner client.Object,
	revision int64,
	phase Phase,
) (bool, error) {
	var numDeleted int
	for _, o := range phase.GetObjects() {
		deleted, err := e.objectEngine.Teardown(ctx, owner, revision, &o)

		if IsTeardownRejectedDueToOwnerOrRevisionChange(err) {
			// not deleted, but not "our" problem anymore.
			numDeleted++
			continue
		}

		if err != nil {
			return false, fmt.Errorf("teardown object: %w", err)
		}
		if deleted {
			numDeleted++
		}
	}
	return numDeleted == len(phase.Objects), nil
}

// Reconcile runs actions to bring actual state closer to desired.
func (e *PhaseEngine) Reconcile(
	ctx context.Context,
	owner client.Object,
	revision int64,
	phase Phase,
) (PhaseResult, error) {
	pres := PhaseResult{
		Name: phase.GetName(),
	}

	// Preflight
	violation, err := e.phaseValidator.Validate(ctx, owner, &phase)
	if err != nil {
		return pres, fmt.Errorf("validating: %w", err)
	}
	if !violation.Empty() {
		pres.PreflightViolation = violation
		return pres, nil
	}

	// Reconcile
	for _, obj := range phase.Objects {
		ores, err := e.objectEngine.Reconcile(ctx, owner, revision, obj.Object, obj.Opts...)
		if err != nil {
			return pres, fmt.Errorf("reconciling object: %w", err)
		}
		pres.Objects = append(pres.Objects, ores)
	}

	return pres, nil
}

// PhaseResult contains information of the state of a reconcile operation.
type PhaseResult struct {
	Name               string
	PreflightViolation validation.PhaseViolation
	Objects            []ObjectResult
}

// Success returnes true when all objects have been successfully reconciled.
func (r PhaseResult) Success() bool {
	if r.PreflightViolation != nil {
		return false
	}

	for _, or := range r.Objects {
		if !or.Success() {
			return false
		}
	}

	return true
}
