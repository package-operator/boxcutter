// Package boxcutter provides a object reconciliation library based on Package Operator.
package boxcutter

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"pkg.package-operator.run/boxcutter/machinery"
	"pkg.package-operator.run/boxcutter/machinery/types"
	"pkg.package-operator.run/boxcutter/validation"
)

// NewRevision creates a new Revision instance using type inference to determine the concrete type of the revision metadata.
func NewRevision[T types.RevisionMetadata](name string, metadata T, revision int64, phases []types.Phase) *RevisionImpl[T] {
	return types.NewRevision(name, metadata, revision, phases)
}

// Revision represents multiple phases at a given point in time.
type Revision = types.Revision

// RevisionImpl is an implementation of the Revision interface whose revision metadata is of concrete type T.
type RevisionImpl[T types.RevisionMetadata] = types.RevisionImpl[T]

// Phase represents a collection of objects lifecycled together.
type Phase = types.Phase

// ObjectReconcileOption is the common interface for object reconciliation options.
type ObjectReconcileOption = types.ObjectReconcileOption

// ObjectTeardownOption holds configuration options changing object teardown.
type ObjectTeardownOption = types.ObjectTeardownOption

// PhaseReconcileOption is the common interface for phase reconciliation options.
type PhaseReconcileOption = types.PhaseReconcileOption

// PhaseTeardownOption holds configuration options changing phase teardown.
type PhaseTeardownOption = types.PhaseTeardownOption

// RevisionReconcileOption is the common interface for revision reconciliation options.
type RevisionReconcileOption = types.RevisionReconcileOption

// RevisionTeardownOption holds configuration options changing revision teardown.
type RevisionTeardownOption = types.RevisionTeardownOption

// WithPreviousOwners is a list of known objects allowed to take ownership from.
// Objects from this list will not trigger collision detection and prevention.
type WithPreviousOwners = types.WithPreviousOwners

const (
	// CollisionProtectionPrevent prevents owner collisions entirely
	// by not allowing to work with objects already present on the cluster.
	CollisionProtectionPrevent = types.CollisionProtectionPrevent
	// CollisionProtectionIfNoController allows to patch and override
	// objects already present if they are not owned by another controller.
	CollisionProtectionIfNoController = types.CollisionProtectionIfNoController
	// CollisionProtectionNone allows to patch and override objects already
	// present and owned by other controllers.
	//
	// Be careful!
	// This setting may cause multiple controllers to fight over a resource,
	// causing load on the Kubernetes API server and etcd.
	CollisionProtectionNone = types.CollisionProtectionNone
)

// WithCollisionProtection instructs the given CollisionProtection setting to be used.
type WithCollisionProtection = types.WithCollisionProtection

// WithPaused skips reconciliation and just reports status information.
// Can also be described as dry-run, as no modification will occur.
type WithPaused = types.WithPaused

// Prober needs to be implemented by any probing implementation.
type Prober = types.Prober

// ProbeFunc wraps the given function to work with the Prober interface.
var ProbeFunc = types.ProbeFunc

// WithProbe registers the given probe to evaluate state of objects.
var WithProbe = types.WithProbe

// WithObjectReconcileOptions applies the given options only to the given object.
var WithObjectReconcileOptions = types.WithObjectReconcileOptions

// WithObjectTeardownOptions applies the given options only to the given object.
var WithObjectTeardownOptions = types.WithObjectTeardownOptions

// WithPhaseReconcileOptions applies the given options only to the given Phase.
var WithPhaseReconcileOptions = types.WithPhaseReconcileOptions

// WithPhaseTeardownOptions applies the given options only to the given Phase.
var WithPhaseTeardownOptions = types.WithPhaseTeardownOptions

// ProgressProbeType is a well-known probe type used to guard phase progression.
const ProgressProbeType = types.ProgressProbeType

// RevisionEngine manages rollout and teardown of multiple phases.
type RevisionEngine = machinery.RevisionEngine

// RevisionMetadata is the interface for managing ownership metadata.
type RevisionMetadata = types.RevisionMetadata

// RevisionReference is the interface for revision reference information.
type RevisionReference = types.RevisionReference

// RevisionEngineOptions holds all configuration options for the RevisionEngine.
type RevisionEngineOptions struct {
	Scheme          *runtime.Scheme
	FieldOwner      string
	SystemPrefix    string
	DiscoveryClient discovery.OpenAPIV3SchemaInterface
	RestMapper      meta.RESTMapper
	Writer          client.Writer
	Reader          client.Reader

	// Optional

	PhaseValidator *validation.PhaseValidator
}

// NewPhaseEngine  returns a new PhaseEngine instance.
func NewPhaseEngine(opts RevisionEngineOptions) (*machinery.PhaseEngine, error) {
	if err := validateRevisionEngineOpts(opts); err != nil {
		return nil, err
	}

	if opts.PhaseValidator == nil {
		opts.PhaseValidator = validation.NewPhaseValidator(opts.RestMapper, opts.Writer)
	}

	comp := machinery.NewComparator(
		opts.DiscoveryClient, opts.Scheme, opts.FieldOwner)

	oe := machinery.NewObjectEngine(
		opts.Scheme, opts.Reader, opts.Writer,
		comp, opts.FieldOwner, opts.SystemPrefix,
	)

	return machinery.NewPhaseEngine(oe, opts.PhaseValidator), nil
}

// NewRevisionEngine returns a new RevisionEngine instance.
func NewRevisionEngine(opts RevisionEngineOptions) (*RevisionEngine, error) {
	if err := validateRevisionEngineOpts(opts); err != nil {
		return nil, err
	}

	pval := opts.PhaseValidator
	if pval == nil {
		pval = validation.NewPhaseValidator(opts.RestMapper, opts.Writer)
	}

	rval := validation.NewRevisionValidator()

	comp := machinery.NewComparator(
		opts.DiscoveryClient, opts.Scheme, opts.FieldOwner)

	oe := machinery.NewObjectEngine(
		opts.Scheme, opts.Reader, opts.Writer,
		comp, opts.FieldOwner, opts.SystemPrefix,
	)
	pe := machinery.NewPhaseEngine(oe, pval)

	return machinery.NewRevisionEngine(pe, rval, opts.Writer), nil
}

// RevisionEngineOptionsError is returned for errors with the RevisionEngineOptions.
type RevisionEngineOptionsError struct {
	msg string
}

func (e RevisionEngineOptionsError) Error() string {
	return e.msg
}

func validateRevisionEngineOpts(opts RevisionEngineOptions) error {
	if opts.Scheme == nil {
		return RevisionEngineOptionsError{msg: "scheme must be provided"}
	}

	if len(opts.FieldOwner) == 0 {
		return RevisionEngineOptionsError{msg: "fieldOwner must be provided"}
	}

	if len(opts.SystemPrefix) == 0 {
		return RevisionEngineOptionsError{msg: "systemPrefix must be provided"}
	}

	if opts.DiscoveryClient == nil {
		return RevisionEngineOptionsError{msg: "discoveryClient must be provided"}
	}

	if opts.RestMapper == nil {
		return RevisionEngineOptionsError{msg: "restMapper must be provided"}
	}

	if opts.Writer == nil {
		return RevisionEngineOptionsError{msg: "writer must be provided"}
	}

	if opts.Reader == nil {
		return RevisionEngineOptionsError{msg: "reader must be provided"}
	}

	return nil
}
