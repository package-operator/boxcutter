package machinery

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ObjectResult is the common Result interface for multiple result types.
type ObjectResult interface {
	// Action taken by the reconcile engine.
	Action() Action
	// Object as last seen on the cluster after creation/update.
	Object() *unstructured.Unstructured
	// Success returns true when the operation is considered successful.
	// Operations are considered a success, when the object reflects desired state,
	// is owned by the right controller and passes the given probe.
	Success() bool
	// Probe returns the results from the given object Probe.
	Probe() ObjectProbeResult
	// String returns a human readable description of the Result.
	String() string
}

// ObjectProbeResult records probe results for the object.
type ObjectProbeResult struct {
	Success  bool
	Messages []string
}

var (
	_ ObjectResult = (*ObjectResultCreated)(nil)
	_ ObjectResult = (*ObjectResultUpdated)(nil)
	_ ObjectResult = (*ObjectResultIdle)(nil)
	_ ObjectResult = (*ObjectResultProgressed)(nil)
	_ ObjectResult = (*ObjectResultRecovered)(nil)
	_ ObjectResult = (*ObjectResultCollision)(nil)
)

// ObjectResultCreated is returned when the Object was just created.
type ObjectResultCreated struct {
	obj         *unstructured.Unstructured
	probeResult ObjectProbeResult
}

func newObjectResultCreated(
	obj *unstructured.Unstructured,
	probe prober,
) ObjectResult {
	s, msgs := probe.Probe(obj)
	return ObjectResultCreated{
		obj: obj,
		probeResult: ObjectProbeResult{
			Success:  s,
			Messages: msgs,
		},
	}
}

// Action taken by the reconcile engine.
func (r ObjectResultCreated) Action() Action {
	return ActionCreated
}

// Object as last seen on the cluster after creation/update.
func (r ObjectResultCreated) Object() *unstructured.Unstructured {
	return r.obj
}

// Success returns true when the operation is considered successful.
// Operations are considered a success, when the object reflects desired state,
// is owned by the right controller and passes the given probe.
func (r ObjectResultCreated) Success() bool {
	return r.probeResult.Success
}

// Probe returns the results from the given object Probe.
func (r ObjectResultCreated) Probe() ObjectProbeResult {
	return r.probeResult
}

// String returns a human readable description of the Result.
func (r ObjectResultCreated) String() string {
	return reportStart(r)
}

// ObjectResultUpdated is returned when the object is updated.
type ObjectResultUpdated struct {
	normalResult
}

func newObjectResultUpdated(
	obj *unstructured.Unstructured,
	diverged DivergeResult,
	probe prober,
) ObjectResult {
	return ObjectResultUpdated{
		normalResult: newNormalObjectResult(ActionUpdated, obj, diverged, probe),
	}
}

// ObjectResultProgressed is returned when the object has been progressed to a newer revision.
type ObjectResultProgressed struct {
	normalResult
}

func newObjectResultProgressed(
	obj *unstructured.Unstructured,
	diverged DivergeResult,
	probe prober,
) ObjectResult {
	return ObjectResultProgressed{
		normalResult: newNormalObjectResult(ActionProgressed, obj, diverged, probe),
	}
}

// ObjectResultIdle is returned when nothing was done.
type ObjectResultIdle struct {
	normalResult
}

func newObjectResultIdle(
	obj *unstructured.Unstructured,
	diverged DivergeResult,
	probe prober,
) ObjectResult {
	return ObjectResultIdle{
		normalResult: newNormalObjectResult(ActionIdle, obj, diverged, probe),
	}
}

// ObjectResultRecovered is returned when the object had to be reset after conflicting with another actor.
type ObjectResultRecovered struct {
	normalResult
}

func newObjectResultRecovered(
	obj *unstructured.Unstructured,
	diverged DivergeResult,
	probe prober,
) ObjectResult {
	return ObjectResultRecovered{
		normalResult: newNormalObjectResult(ActionRecovered, obj, diverged, probe),
	}
}

type normalResult struct {
	action                          Action
	obj                             *unstructured.Unstructured
	updatedFields                   []string
	conflictingFieldManagers        []string
	conflictingFieldsByFieldManager map[string][]string
	probeResult                     ObjectProbeResult
}

func newNormalObjectResult(
	action Action,
	obj *unstructured.Unstructured,
	diverged DivergeResult,
	probe prober,
) normalResult {
	s, msgs := probe.Probe(obj)
	return normalResult{
		obj:                             obj,
		action:                          action,
		updatedFields:                   diverged.Modified(),
		conflictingFieldManagers:        diverged.ConflictingFieldOwners,
		conflictingFieldsByFieldManager: diverged.ConflictingPaths(),
		probeResult: ObjectProbeResult{
			Success:  s,
			Messages: msgs,
		},
	}
}

// Action taken by the reconcile engine.
func (r normalResult) Action() Action {
	return r.action
}

// Object as last seen on the cluster after creation/update.
func (r normalResult) Object() *unstructured.Unstructured {
	return r.obj
}

// Fields that had to be updated to reconcile the object.
func (r normalResult) UpdatedFields() []string {
	return r.updatedFields
}

// Other field managers that have changed fields causing conflicts.
func (r normalResult) ConflictingFieldManagers() []string {
	return r.conflictingFieldManagers
}

// List of field conflicts for each other manager.
func (r normalResult) ConflictingFieldsByFieldManager() map[string][]string {
	return r.conflictingFieldsByFieldManager
}

// Probe returns the results from the given object Probe.
func (r normalResult) Probe() ObjectProbeResult {
	return r.probeResult
}

// Success returns true when the operation is considered successful.
// Operations are considered a success, when the object reflects desired state,
// is owned by the right controller and passes the given probe.
func (r normalResult) Success() bool {
	return r.probeResult.Success
}

// String returns a human readable description of the Result.
func (r normalResult) String() string {
	msg := reportStart(r)

	if len(r.updatedFields) > 0 {
		msg += "Updated:\n"
		for _, uf := range r.updatedFields {
			msg += fmt.Sprintf("- %s\n", uf)
		}
	}
	if len(r.conflictingFieldsByFieldManager) > 0 {
		msg += "Conflicting Field Managers: " + strings.Join(r.conflictingFieldManagers, ",") + "\n"
		for k, v := range r.conflictingFieldsByFieldManager {
			msg += fmt.Sprintf("Fields contested by %q:\n", k)
			for _, f := range v {
				msg += fmt.Sprintf("- %s\n", f)
			}
		}
	}
	return msg
}

// ObjectResultCollision is returned when conflicting with an existing object.
type ObjectResultCollision struct {
	normalResult
	// conflictingOwner is provided when Refusing due to Collision.
	conflictingOwner *metav1.OwnerReference
}

// ConflictingOwner Conflicting owner if Action == RefusingConflict.
func (r ObjectResultCollision) ConflictingOwner() (*metav1.OwnerReference, bool) {
	return r.conflictingOwner, r.conflictingOwner != nil
}

// Success returns true when the operation is considered successful.
// Operations are considered a success, when the object reflects desired state,
// is owned by the right controller and passes the given probe.
func (r ObjectResultCollision) Success() bool {
	return false
}

// String returns a human readable description of the Result.
func (r ObjectResultCollision) String() string {
	msg := r.normalResult.String()
	msg += fmt.Sprintf("Conflicting Owner: %s\n", r.conflictingOwner.String())
	return msg
}

func newObjectResultConflict(
	obj *unstructured.Unstructured,
	diverged DivergeResult,
	conflictingOwner *metav1.OwnerReference,
	probe prober,
) ObjectResult {
	return ObjectResultCollision{
		normalResult: newNormalObjectResult(
			ActionCollision,
			obj, diverged, probe,
		),
		conflictingOwner: conflictingOwner,
	}
}

// Action describes the taken reconciliation action.
type Action string

const (
	// ActionCreated indicates that the object has been created to restore desired state.
	ActionCreated Action = "Created"
	// ActionUpdated indicates that the object has been updated to action on a change in desired state.
	ActionUpdated Action = "Updated"
	// ActionRecovered indicates that the object has been updated to recover values to
	// reflect desired state after interference from another actor of the system.
	ActionRecovered Action = "Recovered"
	// ActionProgressed indicates that the object progressed to newer revision.
	ActionProgressed Action = "Progressed"
	// ActionIdle indicates that no action was necessary. -> NoOp.
	ActionIdle Action = "Idle"
	// ActionCollision indicates aking actions was refused due to a collision with an existing object.
	ActionCollision Action = "Collision"
)

func reportStart(or ObjectResult) string {
	obj := or.Object()
	msg := fmt.Sprintf(
		"Object %s.%s %s/%s\n"+
			`Action: %q`+"\n",
		obj.GetKind(), obj.GetAPIVersion(),
		obj.GetNamespace(), obj.GetName(),
		or.Action(),
	)
	probe := or.Probe()
	if probe.Success {
		msg += "Probe:  Succeeded"
	} else {
		msg += "Probe:  Failed\n"
		for _, m := range probe.Messages {
			msg += "- " + m + "\n"
		}
	}
	return msg
}
