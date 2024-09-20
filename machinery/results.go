package machinery

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"pkg.package-operator.run/boxcutter/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjectIdentity holds information to identify an object.
type ObjectIdentity struct {
	schema.GroupVersionKind
	client.ObjectKey
}

// String returns a string representation.
func (oid ObjectIdentity) String() string {
	return fmt.Sprintf("%s %s", oid.GroupVersionKind, oid.ObjectKey)
}

// Result is the common Result interface for multiple result types.
type Result interface {
	// Object the reconciliation was performed for.
	Identity() ObjectIdentity
	// Action taken by the reconcile engine.
	Action() ObjectAction
	// Returns the probe result.
	ProbeResult() ProbeResult
	// Returns violations when Action == RefusedPreflight.
	PreflightViolations() []validation.Violation
	// Object as last seen on the cluster after creation/update.
	Object() *unstructured.Unstructured
	// Fields that had to be updated to reconcile the object.
	UpdatedFields() []string
	// Other field managers that have changed fields causing conflicts.
	ConflictingFieldManagers() []string
	// List of field conflicts for each other manager.
	ConflictingFieldsByFieldManager() map[string][]string
	// Conflicting owner if Action == RefusingConflict.
	ConflictingOwner() (*metav1.OwnerReference, bool)
	// Returns a human readable description of the result.
	String() string
}

var _ Result = (*objectResultRefusedPreflight)(nil)

type objectResultRefusedPreflight struct {
	id                  ObjectIdentity
	preflightViolations []validation.Violation
}

func newObjectResultRefusedPreflight(
	oid ObjectIdentity,
	preflightViolations []validation.Violation,
) Result {
	return objectResultRefusedPreflight{
		id:                  oid,
		preflightViolations: preflightViolations,
	}
}

func (r objectResultRefusedPreflight) Identity() ObjectIdentity {
	return r.id
}

func (r objectResultRefusedPreflight) Action() ObjectAction {
	return ObjectActionRefusedPreflight
}

func (r objectResultRefusedPreflight) PreflightViolations() []validation.Violation {
	return r.preflightViolations
}

func (r objectResultRefusedPreflight) ProbeResult() ProbeResult {
	panic("RefusedPreflight cannot record probe results")
}

func (r objectResultRefusedPreflight) Object() *unstructured.Unstructured {
	panic("RefusedPreflight has no state of object in-cluster")
}

func (r objectResultRefusedPreflight) UpdatedFields() []string {
	panic("RefusedPreflight cannot record updated fields")
}

func (r objectResultRefusedPreflight) ConflictingFieldManagers() []string {
	panic("RefusedPreflight cannot record conflicting field managers")
}

func (r objectResultRefusedPreflight) ConflictingFieldsByFieldManager() map[string][]string {
	panic("RefusedPreflight cannot record conflicting field managers")
}

func (r objectResultRefusedPreflight) ConflictingOwner() (*metav1.OwnerReference, bool) {
	panic("RefusedPreflight cannot record conflicting owners")
}

func (r objectResultRefusedPreflight) String() string {
	msg := fmt.Sprintf(
		"Object %s %s\n"+
			`Action "RefusedPreflight":`+"\n",
		r.id.GroupVersionKind, r.id.ObjectKey,
	)
	for _, v := range r.preflightViolations {
		msg += fmt.Sprintf("- %s\n", v.String())
	}
	return msg
}

var _ Result = (*objectResultCreated)(nil)

type objectResultCreated struct {
	id    ObjectIdentity
	probe ProbeResult
	obj   *unstructured.Unstructured
}

func newObjectResultCreated(
	oid ObjectIdentity,
	probe ProbeResult,
	obj *unstructured.Unstructured,
) Result {
	return objectResultCreated{
		id:    oid,
		probe: probe,
		obj:   obj,
	}
}

func (r objectResultCreated) Identity() ObjectIdentity {
	return r.id
}

func (r objectResultCreated) Action() ObjectAction {
	return ObjectActionCreated
}

func (r objectResultCreated) PreflightViolations() []validation.Violation {
	return nil
}

func (r objectResultCreated) ProbeResult() ProbeResult {
	return r.probe
}

func (r objectResultCreated) Object() *unstructured.Unstructured {
	return r.obj
}

func (r objectResultCreated) UpdatedFields() []string {
	// TODO: Actually report all fields?
	panic("Created does not record updating ALL fields")
}

func (r objectResultCreated) ConflictingFieldManagers() []string {
	return nil
}

func (r objectResultCreated) ConflictingFieldsByFieldManager() map[string][]string {
	return nil
}

func (r objectResultCreated) ConflictingOwner() (*metav1.OwnerReference, bool) {
	panic("Created cannot have conflicting owners")
}

func (r objectResultCreated) String() string {
	msg := fmt.Sprintf(
		"Object %s %s\n"+
			`Action "Created":`+"\n"+
			"Probe %t: %q\n",
		r.id.GroupVersionKind, r.id.ObjectKey,
		r.probe.Success, r.probe.Message,
	)
	return msg
}

// ProbeResult holds the output of the Prober, if defined.
type ProbeResult struct {
	Success bool
	Message string
}

var _ Result = (*normalResult)(nil)

type normalResult struct {
	id                              ObjectIdentity
	probe                           ProbeResult
	obj                             *unstructured.Unstructured
	action                          ObjectAction
	updatedFields                   []string
	conflictingFieldManagers        []string
	conflictingFieldsByFieldManager map[string][]string
}

func (r normalResult) Identity() ObjectIdentity {
	return r.id
}

func (r normalResult) Action() ObjectAction {
	return r.action
}

func (r normalResult) PreflightViolations() []validation.Violation {
	return nil
}

func (r normalResult) ProbeResult() ProbeResult {
	return r.probe
}

func (r normalResult) Object() *unstructured.Unstructured {
	return r.obj
}

func (r normalResult) UpdatedFields() []string {
	return r.updatedFields
}

func (r normalResult) ConflictingFieldManagers() []string {
	return r.conflictingFieldManagers
}

func (r normalResult) ConflictingFieldsByFieldManager() map[string][]string {
	return r.conflictingFieldsByFieldManager
}

func (r normalResult) ConflictingOwner() (*metav1.OwnerReference, bool) {
	panic(fmt.Sprintf("%s cannot have conflicting owners", r.action))
}

func (r normalResult) String() string {
	msg := fmt.Sprintf(
		"Object %s %s\n"+
			"Action %q\n"+
			"Probe %t: %q\n",
		r.id.GroupVersionKind, r.id.ObjectKey,
		r.action,
		r.probe.Success, r.probe.Message,
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

func newObjectResultProgressed(
	oid ObjectIdentity,
	probe ProbeResult,
	obj *unstructured.Unstructured,
	diverged DivergeResult,
) Result {
	return newNormalResult(ObjectActionProgressed, oid, probe, obj, diverged)
}

func newObjectResultIdle(
	oid ObjectIdentity,
	probe ProbeResult,
	obj *unstructured.Unstructured,
	diverged DivergeResult,
) Result {
	return newNormalResult(ObjectActionIdle, oid, probe, obj, diverged)
}

func newObjectResultUpdated(
	oid ObjectIdentity,
	probe ProbeResult,
	obj *unstructured.Unstructured,
	diverged DivergeResult,
) Result {
	return newNormalResult(ObjectActionUpdated, oid, probe, obj, diverged)
}

func newObjectResultRecovered(
	oid ObjectIdentity,
	probe ProbeResult,
	obj *unstructured.Unstructured,
	diverged DivergeResult,
) Result {
	return newNormalResult(ObjectActionRecovered, oid, probe, obj, diverged)
}

func newNormalResult(
	action ObjectAction,
	oid ObjectIdentity,
	probe ProbeResult,
	obj *unstructured.Unstructured,
	diverged DivergeResult,
) normalResult {
	return normalResult{
		id:                              oid,
		probe:                           probe,
		obj:                             obj,
		action:                          action,
		updatedFields:                   diverged.Modified(),
		conflictingFieldManagers:        diverged.ConflictingFieldOwners,
		conflictingFieldsByFieldManager: diverged.ConflictingPaths(),
	}
}

type objectResultCollision struct {
	normalResult
	// conflictingOwner is provided when Refusing due to Collision.
	conflictingOwner *metav1.OwnerReference
}

func (r objectResultCollision) ConflictingOwner() (*metav1.OwnerReference, bool) {
	return r.conflictingOwner, r.conflictingOwner != nil
}

func (r objectResultCollision) String() string {
	msg := r.normalResult.String()
	msg += fmt.Sprintf("Conflicting Owner: %s\n", r.conflictingOwner.String())
	return msg
}

func newObjectResultConflict(
	oid ObjectIdentity,
	probe ProbeResult,
	obj *unstructured.Unstructured,
	diverged DivergeResult,
	conflictingOwner *metav1.OwnerReference,
) Result {
	return objectResultCollision{
		normalResult: newNormalResult(
			ObjectActionRefusedCollision,
			oid, probe, obj, diverged,
		),
		conflictingOwner: conflictingOwner,
	}
}

// ObjectAction describes the taken reconciliation action.
type ObjectAction string

const (
	// ObjectActionCreated indicates that the object has been created to restore desired state.
	ObjectActionCreated ObjectAction = "Created"
	// ObjectActionUpdated indicates that the object has been updated to action on a change in desired state.
	ObjectActionUpdated ObjectAction = "Updated"
	// ObjectActionRecovered indicates that the object has been updated to recover values to
	// reflect desired state after interference from another actor of the system.
	ObjectActionRecovered ObjectAction = "Recovered"
	// ObjectActionProgressed indicates that the object progressed to newer revision.
	ObjectActionProgressed ObjectAction = "Progressed"
	// ObjectActionIdle indicates that no action was necessary. -> NoOp.
	ObjectActionIdle ObjectAction = "Idle"
	// ObjectActionRefusedCollision indicates aking actions was refused due to a collision with an existing object.
	ObjectActionRefusedCollision ObjectAction = "RefusedCollision"
	// ObjectActionRefusedPreflight indicates Taking actions was refused due failing preflight checks.
	ObjectActionRefusedPreflight ObjectAction = "RefusedPreflight"
)
