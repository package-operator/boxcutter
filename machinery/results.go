package machinery

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Identity holds information to identify an object.
type Identity struct {
	schema.GroupVersionKind
	client.ObjectKey
}

// IdentityForUnstructured returns an Identity object from unstructured.
func IdentityForUnstructured(obj *unstructured.Unstructured) Identity {
	return Identity{
		GroupVersionKind: obj.GroupVersionKind(),
		ObjectKey:        client.ObjectKeyFromObject(obj),
	}
}

// String returns a string representation.
func (oid Identity) String() string {
	return fmt.Sprintf("%s %s", oid.GroupVersionKind, oid.ObjectKey)
}

// Result is the common Result interface for multiple result types.
type Result interface {
	// Object the reconciliation was performed for.
	Identity() Identity
	// Action taken by the reconcile engine.
	Action() Action
	// Object as last seen on the cluster after creation/update.
	Object() *unstructured.Unstructured
	// Success returns true when the operation is considered successful.
	// Operations are considered a success, when the object reflects desired state,
	// is owned by the right controller and passes the given probe.
	Success() bool
	// Probe returns the results from the given object Probe.
	Probe() ProbeResult
	// String returns a human readable description of the Result.
	String() string
}

// ProbeResult records probe results for the object.
type ProbeResult struct {
	Success  bool
	Messages []string
}

var (
	_ Result = (*ResultCreated)(nil)
	_ Result = (*ResultUpdated)(nil)
	_ Result = (*ResultIdle)(nil)
	_ Result = (*ResultProgressed)(nil)
	_ Result = (*ResultRecovered)(nil)
	_ Result = (*ResultCollision)(nil)
)

// ResultCreated is returned when the Object was just created.
type ResultCreated struct {
	id          Identity
	obj         *unstructured.Unstructured
	probeResult ProbeResult
}

func newResultCreated(
	oid Identity,
	obj *unstructured.Unstructured,
	probe prober,
) Result {
	s, msgs := probe.Probe(obj)
	return ResultCreated{
		id:  oid,
		obj: obj,
		probeResult: ProbeResult{
			Success:  s,
			Messages: msgs,
		},
	}
}

// Identity returns object the reconciliation was performed for.
func (r ResultCreated) Identity() Identity {
	return r.id
}

// Action taken by the reconcile engine.
func (r ResultCreated) Action() Action {
	return ActionCreated
}

// Object as last seen on the cluster after creation/update.
func (r ResultCreated) Object() *unstructured.Unstructured {
	return r.obj
}

// Success returns true when the operation is considered successful.
// Operations are considered a success, when the object reflects desired state,
// is owned by the right controller and passes the given probe.
func (r ResultCreated) Success() bool {
	return r.probeResult.Success
}

// Probe returns the results from the given object Probe.
func (r ResultCreated) Probe() ProbeResult {
	return r.probeResult
}

// String returns a human readable description of the Result.
func (r ResultCreated) String() string {
	msg := fmt.Sprintf(
		"Object %s\n"+
			`Action "Created":`+"\n",
		r.id.String(),
	)
	return msg
}

// ResultUpdated is returned when the object is updated.
type ResultUpdated struct {
	normalResult
}

func newResultUpdated(
	oid Identity,
	obj *unstructured.Unstructured,
	diverged DivergeResult,
	probe prober,
) Result {
	return ResultUpdated{
		normalResult: newNormalResult(ActionUpdated, oid, obj, diverged, probe),
	}
}

// ResultProgressed is returned when the object has been progressed to a newer revision.
type ResultProgressed struct {
	normalResult
}

func newResultProgressed(
	oid Identity,
	obj *unstructured.Unstructured,
	diverged DivergeResult,
	probe prober,
) Result {
	return ResultProgressed{
		normalResult: newNormalResult(ActionProgressed, oid, obj, diverged, probe),
	}
}

// ResultIdle is returned when nothing was done.
type ResultIdle struct {
	normalResult
}

func newResultIdle(
	oid Identity,
	obj *unstructured.Unstructured,
	diverged DivergeResult,
	probe prober,
) Result {
	return ResultIdle{
		normalResult: newNormalResult(ActionIdle, oid, obj, diverged, probe),
	}
}

// ResultRecovered is returned when the object had to be reset after conflicting with another actor.
type ResultRecovered struct {
	normalResult
}

func newResultRecovered(
	oid Identity,
	obj *unstructured.Unstructured,
	diverged DivergeResult,
	probe prober,
) Result {
	return ResultRecovered{
		normalResult: newNormalResult(ActionRecovered, oid, obj, diverged, probe),
	}
}

type normalResult struct {
	id                              Identity
	action                          Action
	obj                             *unstructured.Unstructured
	updatedFields                   []string
	conflictingFieldManagers        []string
	conflictingFieldsByFieldManager map[string][]string
	probeResult                     ProbeResult
}

func newNormalResult(
	action Action,
	oid Identity,
	obj *unstructured.Unstructured,
	diverged DivergeResult,
	probe prober,
) normalResult {
	s, msgs := probe.Probe(obj)
	return normalResult{
		id:                              oid,
		obj:                             obj,
		action:                          action,
		updatedFields:                   diverged.Modified(),
		conflictingFieldManagers:        diverged.ConflictingFieldOwners,
		conflictingFieldsByFieldManager: diverged.ConflictingPaths(),
		probeResult: ProbeResult{
			Success:  s,
			Messages: msgs,
		},
	}
}

// Identity returns object the reconciliation was performed for.
func (r normalResult) Identity() Identity {
	return r.id
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
func (r normalResult) Probe() ProbeResult {
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
	msg := fmt.Sprintf(
		"Object %s %s\n"+
			"Action %q\n",
		r.id.GroupVersionKind, r.id.ObjectKey,
		r.action,
	)
	if len(r.updatedFields) > 0 {
		msg += "Updated Fields:\n"
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

// ResultCollision is returned when conflicting with an existing object.
type ResultCollision struct {
	normalResult
	// conflictingOwner is provided when Refusing due to Collision.
	conflictingOwner *metav1.OwnerReference
}

// ConflictingOwner Conflicting owner if Action == RefusingConflict.
func (r ResultCollision) ConflictingOwner() (*metav1.OwnerReference, bool) {
	return r.conflictingOwner, r.conflictingOwner != nil
}

// Success returns true when the operation is considered successful.
// Operations are considered a success, when the object reflects desired state,
// is owned by the right controller and passes the given probe.
func (r ResultCollision) Success() bool {
	return false
}

// String returns a human readable description of the Result.
func (r ResultCollision) String() string {
	msg := r.normalResult.String()
	msg += fmt.Sprintf("Conflicting Owner: %s\n", r.conflictingOwner.String())
	return msg
}

func newResultConflict(
	oid Identity,
	obj *unstructured.Unstructured,
	diverged DivergeResult,
	conflictingOwner *metav1.OwnerReference,
	probe prober,
) Result {
	return ResultCollision{
		normalResult: newNormalResult(
			ActionCollision,
			oid, obj, diverged, probe,
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
