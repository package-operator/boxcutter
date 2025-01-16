package machinery

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"pkg.package-operator.run/boxcutter/machinery/types"
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
		desiredObject Object,
		opts ...ObjectOption,
	) (ObjectResult, error)
	Teardown(
		ctx context.Context,
		owner client.Object, // Owner of the object.
		revision int64, // Revision number, must start at 1.
		desiredObject Object,
	) (objectGone bool, err error)
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

// PhaseTeardownResult interface to access results of phase teardown.
type PhaseTeardownResult interface {
	GetName() string
	// IsComplete returns true when all objects have been deleted,
	// finalizers have been processes and the objects are longer
	// present on the kube-apiserver.
	IsComplete() bool
	// Gone returns a list of objects that have been confirmed
	// to be gone from the kube-apiserver.
	Gone() []types.ObjectRef
	// Waiting returns a list of objects that have yet to be
	// cleaned up on the kube-apiserver.
	Waiting() []types.ObjectRef

	String() string
}

type phaseTeardownResult struct {
	name    string
	gone    []types.ObjectRef
	waiting []types.ObjectRef
}

func (r *phaseTeardownResult) String() string {
	out := fmt.Sprintf("Phase %q\n", r.name)

	if len(r.gone) > 0 {
		out += "Gone Objects:\n"
		for _, gone := range r.gone {
			out += "- " + gone.String() + "\n"
		}
	}
	if len(r.waiting) > 0 {
		out += "Waiting Objects:\n"
		for _, waiting := range r.waiting {
			out += "- " + waiting.String() + "\n"
		}
	}
	return out
}

func (r *phaseTeardownResult) GetName() string {
	return r.name
}

// IsComplete returns true when all objects have been deleted,
// finalizers have been processes and the objects are longer
// present on the kube-apiserver.
func (r *phaseTeardownResult) IsComplete() bool {
	return len(r.waiting) == 0
}

// Gone returns a list of objects that have been confirmed
// to be gone from the kube-apiserver.
func (r *phaseTeardownResult) Gone() []types.ObjectRef {
	return r.gone
}

// Waiting returns a list of objects that have yet to be
// cleaned up on the kube-apiserver.
func (r *phaseTeardownResult) Waiting() []types.ObjectRef {
	return r.waiting
}

// Teardown ensures the given phase is safely removed from the cluster.
func (e *PhaseEngine) Teardown(
	ctx context.Context,
	owner client.Object,
	revision int64,
	phase Phase,
) (PhaseTeardownResult, error) {
	res := &phaseTeardownResult{name: phase.Name}

	for _, o := range phase.GetObjects() {
		gone, err := e.objectEngine.Teardown(ctx, owner, revision, &o)

		if IsTeardownRejectedDueToOwnerOrRevisionChange(err) {
			// not deleted, but not "our" problem anymore.
			res.gone = append(res.gone, types.ToObjectRef(&o))
			continue
		}
		if err != nil {
			return res, fmt.Errorf("teardown object: %w", err)
		}
		if gone {
			res.gone = append(res.gone, types.ToObjectRef(&o))
		} else {
			res.waiting = append(res.waiting, types.ToObjectRef(&o))
		}
	}
	return res, nil
}

// Reconcile runs actions to bring actual state closer to desired.
func (e *PhaseEngine) Reconcile(
	ctx context.Context,
	owner client.Object,
	revision int64,
	phase Phase,
) (PhaseResult, error) {
	pres := &phaseResult{
		name: phase.GetName(),
	}

	// Preflight
	violation, err := e.phaseValidator.Validate(ctx, owner, &phase)
	if err != nil {
		return pres, fmt.Errorf("validating: %w", err)
	}
	if !violation.Empty() {
		pres.preflightViolation = violation
		return pres, nil
	}

	// Reconcile
	for _, obj := range phase.Objects {
		ores, err := e.objectEngine.Reconcile(ctx, owner, revision, obj.Object, obj.Opts...)
		if err != nil {
			return pres, fmt.Errorf("reconciling object: %w", err)
		}
		pres.objects = append(pres.objects, ores)
	}

	return pres, nil
}

// PhaseResult interface to access results of phase reconcile.
type PhaseResult interface {
	// GetName returns the name of the phase.
	GetName() string
	// GetPreflightViolation returns the preflight
	// violation, if one was encountered.
	GetPreflightViolation() (validation.PhaseViolation, bool)
	// GetObjects returns results for individual objects.
	GetObjects() []ObjectResult
	// InTransition returns true if the Phase has not yet fully rolled out,
	// if the phase has objects progressed to a new revision or
	// if objects have unresolved conflicts.
	InTransistion() bool
	// IsComplete returns true when all objects have
	// successfully been reconciled and pass their probes.
	IsComplete() bool
	String() string
}

// phaseResult contains information of the state of a reconcile operation.
type phaseResult struct {
	name               string
	preflightViolation validation.PhaseViolation
	objects            []ObjectResult
}

// GetName returns the name of the phase.
func (r *phaseResult) GetName() string {
	return r.name
}

// GetPreflightViolation returns the preflight
// violation, if one was encountered.
func (r *phaseResult) GetPreflightViolation() (validation.PhaseViolation, bool) {
	return r.preflightViolation,
		r.preflightViolation != nil && !r.preflightViolation.Empty()
}

// GetObjects returns results for individual objects.
func (r *phaseResult) GetObjects() []ObjectResult {
	return r.objects
}

// InTransition returns true if the Phase has not yet fully rolled out,
// if the phase has objects progressed to a new revision or
// if objects have unresolved conflicts.
func (r *phaseResult) InTransistion() bool {
	if _, ok := r.GetPreflightViolation(); ok {
		return false
	}
	for _, o := range r.objects {
		switch o.Action() {
		case ActionCollision, ActionProgressed:
			return true
		}
	}
	return false
}

// IsComplete returns true when all objects have
// successfully been reconciled and pass their probes.
func (r *phaseResult) IsComplete() bool {
	if _, ok := r.GetPreflightViolation(); ok {
		return false
	}
	for _, o := range r.objects {
		if o.Action() == ActionCollision {
			return false
		}
		if !o.Probe().Success {
			return false
		}
	}
	return true
}

func (r *phaseResult) String() string {
	out := fmt.Sprintf(
		"Phase %q\nComplete: %t\nIn Transition: %t\n",
		r.name, r.IsComplete(), r.InTransistion(),
	)

	if v, ok := r.GetPreflightViolation(); ok {
		out += "Preflight Violation:\n"
		out += "  " + strings.ReplaceAll(strings.TrimSpace(v.String()), "\n", "\n  ") + "\n"
	}

	out += "Objects:\n"
	for _, ores := range r.objects {
		out += "- " + strings.ReplaceAll(strings.TrimSpace(ores.String()), "\n", "\n  ") + "\n"
	}
	return out
}
