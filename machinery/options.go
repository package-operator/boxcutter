package machinery

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjectOptions holds configuration options changing object reconciliation.
type ObjectOptions struct {
	CollisionProtection CollisionProtection
	PreviousOwners      []client.Object
	Paused              bool
	Prober              Prober
}

// Default sets empty ObjectOption fields to their default value.
func (opts *ObjectOptions) Default() {
	if opts.Prober == nil {
		opts.Prober = &noopProbe{}
	}
	if len(opts.CollisionProtection) == 0 {
		opts.CollisionProtection = CollisionProtectionPrevent
	}
}

// ObjectOption is the common interface for object reconciliation options.
type ObjectOption interface {
	ApplyToObjectOptions(opts *ObjectOptions)
}

// CollisionProtection specifies how collision with existing objects and
// other controllers should be handled.
type CollisionProtection string

const (
	// CollisionProtectionPrevent prevents owner collisions entirely
	// by not allowing to work with objects already present on the cluster.
	CollisionProtectionPrevent CollisionProtection = "Prevent"
	// CollisionProtectionIfNoController allows to patch and override
	// objects already present if they are not owned by another controller.
	CollisionProtectionIfNoController CollisionProtection = "IfNoController"
	// CollisionProtectionNone allows to patch and override objects already
	// present and owned by other controllers.
	//
	// Be careful!
	// This setting may cause multiple controllers to fight over a resource,
	// causing load on the Kubernetes API server and etcd.
	CollisionProtectionNone CollisionProtection = "None"
)

// WithCollisionProtection instructs the given CollisionProtection setting to be used.
type WithCollisionProtection CollisionProtection

// ApplyToObjectOptions implements ObjectOption.
func (p WithCollisionProtection) ApplyToObjectOptions(opts *ObjectOptions) {
	opts.CollisionProtection = CollisionProtection(p)
}

// WithPreviousOwners is a list of known objects allowed to take ownership from.
// Objects from this list will not trigger collision detection and prevention.
type WithPreviousOwners []client.Object

// ApplyToObjectOptions implements ObjectOption.
func (p WithPreviousOwners) ApplyToObjectOptions(opts *ObjectOptions) {
	opts.PreviousOwners = p
}

// WithPaused skips reconciliation and just reports status information.
// Can also be described as dry-run, as no modification will occur.
type WithPaused struct{}

// ApplyToObjectOptions implements ObjectOption.
func (p WithPaused) ApplyToObjectOptions(opts *ObjectOptions) {
	opts.Paused = true
}

// Prober check Kubernetes objects for certain conditions and report success or failure with a failure message.
type Prober interface {
	Probe(obj *unstructured.Unstructured) (success bool, message string)
}

// WithProber adds a probing function executed against the latest state of an object.
type WithProber struct{ Prober }

// ApplyToObjectOptions implements ObjectOption.
func (p WithProber) ApplyToObjectOptions(opts *ObjectOptions) {
	opts.Prober = p.Prober
}

type noopProbe struct{}

func (p noopProbe) Probe(_ *unstructured.Unstructured) (success bool, message string) {
	return true, ""
}
