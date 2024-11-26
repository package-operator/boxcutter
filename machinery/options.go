package machinery

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjectOptions holds configuration options changing object reconciliation.
type ObjectOptions struct {
	CollisionProtection CollisionProtection
	PreviousOwners      []client.Object
	Probe               prober
	Paused              bool
}

// Default sets empty Option fields to their default value.
func (opts *ObjectOptions) Default() {
	if len(opts.CollisionProtection) == 0 {
		opts.CollisionProtection = CollisionProtectionPrevent
	}
	if opts.Probe == nil {
		opts.Probe = &noopProbe{}
	}
}

// ObjectOption is the common interface for object reconciliation options.
type ObjectOption interface {
	ApplyToObjectOptions(opts *ObjectOptions)
}

var (
	_ ObjectOption = (WithCollisionProtection)("")
	_ ObjectOption = (WithPaused{})
	_ ObjectOption = (WithPreviousOwners{})
	_ ObjectOption = (WithProbe{})
)

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

// ApplyToObjectOptions implements Option.
func (p WithCollisionProtection) ApplyToObjectOptions(opts *ObjectOptions) {
	opts.CollisionProtection = CollisionProtection(p)
}

// WithPreviousOwners is a list of known objects allowed to take ownership from.
// Objects from this list will not trigger collision detection and prevention.
type WithPreviousOwners []client.Object

// ApplyToObjectOptions implements Option.
func (p WithPreviousOwners) ApplyToObjectOptions(opts *ObjectOptions) {
	opts.PreviousOwners = p
}

// WithPaused skips reconciliation and just reports status information.
// Can also be described as dry-run, as no modification will occur.
type WithPaused struct{}

// ApplyToObjectOptions implements Option.
func (p WithPaused) ApplyToObjectOptions(opts *ObjectOptions) {
	opts.Paused = true
}

type prober interface {
	Probe(obj client.Object) (success bool, messages []string)
}

// WithProbe executes the given probe to evaluate the state of the object.
type WithProbe struct{ Probe prober }

// ApplyToObjectOptions implements Option.
func (p WithProbe) ApplyToObjectOptions(opts *ObjectOptions) {
	opts.Probe = p.Probe
}

type noopProbe struct{}

func (p *noopProbe) Probe(_ client.Object) (success bool, messages []string) {
	return true, nil
}
