package machinery

import (
	"context"
	"fmt"
	"slices"

	"pkg.package-operator.run/boxcutter/machinery/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RevisionEngine manages rollout and teardown of multiple phases.
type RevisionEngine struct {
	phaseEngine       phaseEngine
	revisionValidator revisionValidator
}

// NewRevisionEngine returns a new RevisionEngine instance.
func NewRevisionEngine(
	phaseEngine phaseEngine,
	revisionValidator revisionValidator,
) *RevisionEngine {
	return &RevisionEngine{
		phaseEngine:       phaseEngine,
		revisionValidator: revisionValidator,
	}
}

type revisionValidator interface {
	Validate(_ context.Context, rev Revision) (validation.RevisionViolation, error)
}

type phaseEngine interface {
	Reconcile(
		ctx context.Context,
		owner client.Object,
		revision int64,
		phase Phase,
	) (PhaseResult, error)
	Teardown(
		ctx context.Context,
		owner client.Object,
		revision int64,
		phase Phase,
	) (PhaseTeardownResult, error)
}

// Revision represents multiple phases at a given point in time.
type Revision struct {
	Owner    client.Object
	Revision int64
	Phases   []Phase
}

// GetPhases returns the phases the revision is rolling out.
func (r Revision) GetPhases() []validation.Phase {
	phases := make([]validation.Phase, len(r.Phases))
	for i := range r.Phases {
		phases[i] = &r.Phases[i]
	}
	return phases
}

// RevisionResult holds details about the revision reconciliation run.
type RevisionResult interface {
	// GetPreflightViolation returns the preflight
	// violation, if one was encountered.
	GetPreflightViolation() (validation.RevisionViolation, bool)
	// GetPhases returns results for individual phases.
	GetPhases() []PhaseResult
	// InTransition returns true if the Phase has not yet fully rolled out,
	// if the phase has objects progressed to a new revision or
	// if objects have unresolved conflicts.
	InTransistion() bool
	// IsComplete returns true when all objects have
	// successfully been reconciled and pass their probes.
	IsComplete() bool
}

type revisionResult struct {
	numberOfPhases     int
	phases             []PhaseResult
	preflightViolation validation.RevisionViolation
}

// GetPreflightViolation returns the preflight
// violation, if one was encountered.
func (r *revisionResult) GetPreflightViolation() (validation.RevisionViolation, bool) {
	return r.preflightViolation,
		r.preflightViolation != nil && !r.preflightViolation.Empty()
}

// InTransition returns true if the Phase has not yet fully rolled out,
// if the phase has objects progressed to a new revision or
// if objects have unresolved conflicts.
func (r *revisionResult) InTransistion() bool {
	if len(r.phases) < r.numberOfPhases {
		// not all phases have been acted on.
		return true
	}
	for _, p := range r.phases {
		if p.InTransistion() {
			return true
		}
	}
	return false
}

// IsComplete returns true when all phases have
// successfully been reconciled and pass their probes.
func (r *revisionResult) IsComplete() bool {
	if len(r.phases) < r.numberOfPhases {
		// not all phases have been acted on.
		return false
	}
	for _, p := range r.phases {
		if !p.IsComplete() {
			return false
		}
	}
	return true
}

// GetPhases returns results for individual phases.
func (r *revisionResult) GetPhases() []PhaseResult {
	return r.phases
}

// Reconcile runs actions to bring actual state closer to desired.
func (re *RevisionEngine) Reconcile(
	ctx context.Context, rev Revision,
) (RevisionResult, error) {
	rres := &revisionResult{
		numberOfPhases: len(rev.Phases),
	}

	// Preflight
	violation, err := re.revisionValidator.Validate(ctx, rev)
	if err != nil {
		return rres, fmt.Errorf("validating: %w", err)
	}
	if !violation.Empty() {
		rres.preflightViolation = violation
		return rres, nil
	}

	// Reconcile
	for _, phase := range rev.Phases {
		pres, err := re.phaseEngine.Reconcile(ctx, rev.Owner, rev.Revision, phase)
		if err != nil {
			return rres, fmt.Errorf("reconciling object: %w", err)
		}
		rres.phases = append(rres.phases, pres)
		if !pres.IsComplete() {
			// Wait
			return rres, nil
		}
	}

	return rres, nil
}

// RevisionTeardownResult holds the results of a Teardown operation.
type RevisionTeardownResult interface {
	// GetPhases returns results for individual phases.
	GetPhases() []PhaseTeardownResult
	// IsComplete returns true when all objects have been deleted,
	// finalizers have been processes and the objects are longer
	// present on the kube-apiserver.
	IsComplete() bool
	// GetWaitingPhaseNames returns a list of phase names waiting
	// to be torn down.
	GetWaitingPhaseNames() []string
	// GetActivePhaseName returns the name of the phase that is
	// currently being torn down (e.g. waiting on finalizers).
	// Second return is false when no phase is active.
	GetActivePhaseName() (string, bool)
	// GetGonePhaseNames returns a list of phase names already processed.
	GetGonePhaseNames() []string
}

type revisionTeardownResult struct {
	phases  []PhaseTeardownResult
	active  string
	waiting []string
	gone    []string
}

// GetPhases returns results for individual phases.
func (r *revisionTeardownResult) GetPhases() []PhaseTeardownResult {
	return r.phases
}

// IsComplete returns true when all objects have been deleted,
// finalizers have been processes and the objects are longer
// present on the kube-apiserver.
func (r *revisionTeardownResult) IsComplete() bool {
	return len(r.waiting) == 0 && len(r.active) == 0
}

// GetWaitingPhaseNames returns a list of phase names waiting
// to be torn down.
func (r *revisionTeardownResult) GetWaitingPhaseNames() []string {
	return r.waiting
}

// GetActivePhaseName returns the name of the phase that is
// currently being torn down (e.g. waiting on finalizers).
// Second return is false when no phase is active.
func (r *revisionTeardownResult) GetActivePhaseName() (string, bool) {
	return r.active, len(r.active) > 0
}

// GetGonePhaseNames returns a list of phase names already processed.
func (r *revisionTeardownResult) GetGonePhaseNames() []string {
	return r.gone
}

// Teardown ensures the given revision is safely removed from the cluster.
func (re *RevisionEngine) Teardown(
	ctx context.Context, rev Revision,
) (RevisionTeardownResult, error) {
	res := &revisionTeardownResult{}

	waiting := map[string]struct{}{}
	for _, p := range rev.Phases {
		waiting[p.Name] = struct{}{}
	}

	// Phases should be torn down in reverse.
	reversedPhases := slices.Clone(rev.Phases)
	slices.Reverse(reversedPhases)

	for _, p := range reversedPhases {
		// Phase is no longer waiting.
		delete(waiting, p.Name)
		res.active = p.Name

		pres, err := re.phaseEngine.Teardown(ctx, rev.Owner, rev.Revision, p)
		if err != nil {
			return nil, fmt.Errorf("teardown phase: %w", err)
		}

		res.phases = append(res.phases, pres)
		if pres.IsComplete() {
			res.gone = append(res.gone, p.Name)
			continue
		}

		// record other phases as waiting in normal order.
		for _, p := range rev.Phases {
			if _, ok := waiting[p.Name]; ok {
				res.waiting = append(res.waiting, p.Name)
			}
		}
		slices.Reverse(res.gone)
		return res, nil
	}

	slices.Reverse(res.gone)
	res.active = ""
	return res, nil
}
