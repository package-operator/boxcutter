package types

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PhaseOptions struct {
	// DefaultObjectOptions applying to all objects in the phase.
	DefaultObjectOptions []ObjectOption
	// ObjectOptions maps ObjectOptions for specific objects.s
	ObjectOptions map[ObjectRef][]ObjectOption
}

type PhaseOption interface {
	ApplyToPhaseOptions(opts *PhaseOptions)
}

// ObjectOptions holds configuration options changing object reconciliation.
type ObjectOptions struct {
	CollisionProtection CollisionProtection
	PreviousOwners      []client.Object
	Paused              bool
	Probes              map[string]Prober
}

// Default sets empty Option fields to their default value.
func (opts *ObjectOptions) Default() {
	if len(opts.CollisionProtection) == 0 {
		opts.CollisionProtection = CollisionProtectionPrevent
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
	_ ObjectOption = (WithProbe("", nil))
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

// ApplyToObjectOptions implements ObjectOption.
func (p WithCollisionProtection) ApplyToObjectOptions(opts *ObjectOptions) {
	opts.CollisionProtection = CollisionProtection(p)
}

// ApplyToPhaseOptions implements PhaseOption.
func (p WithCollisionProtection) ApplyToPhaseOptions(opts *PhaseOptions) {
	opts.DefaultObjectOptions = append(opts.DefaultObjectOptions, p)
}

// WithPreviousOwners is a list of known objects allowed to take ownership from.
// Objects from this list will not trigger collision detection and prevention.
type WithPreviousOwners []client.Object

// ApplyToObjectOptions implements ObjectOption.
func (p WithPreviousOwners) ApplyToObjectOptions(opts *ObjectOptions) {
	opts.PreviousOwners = p
}

// ApplyToPhaseOptions implements PhaseOption.
func (p WithPreviousOwners) ApplyToPhaseOptions(opts *PhaseOptions) {
	opts.DefaultObjectOptions = append(opts.DefaultObjectOptions, p)
}

// WithPaused skips reconciliation and just reports status information.
// Can also be described as dry-run, as no modification will occur.
type WithPaused struct{}

// ApplyToObjectOptions implements ObjectOption.
func (p WithPaused) ApplyToObjectOptions(opts *ObjectOptions) {
	opts.Paused = true
}

// ApplyToPhaseOptions implements PhaseOption.
func (p WithPaused) ApplyToPhaseOptions(opts *PhaseOptions) {
	opts.DefaultObjectOptions = append(opts.DefaultObjectOptions, p)
}

// ProgressProbeType is a well-known probe type used to guard phase progression.
const ProgressProbeType = "Progress"

// Prober needs to be implemented by any probing implementation.
type Prober interface {
	Probe(obj client.Object) (success bool, messages []string)
}

// ProbeFunc wraps the given function to work with the Prober interface.
func ProbeFunc(fn func(obj client.Object) (success bool, messages []string)) Prober {
	return &probeFn{Fn: fn}
}

type probeFn struct {
	Fn func(obj client.Object) (success bool, messages []string)
}

func (p *probeFn) Probe(obj client.Object) (success bool, messages []string) {
	return p.Fn(obj)
}

// WithProbe registers the given probe to evaluate state of objects.
func WithProbe(t string, probe Prober) *optionFn {
	return &optionFn{
		fn: func(opts *ObjectOptions) {
			if opts.Probes == nil {
				opts.Probes = map[string]Prober{}
			}
			opts.Probes[t] = probe
		},
	}
}

type withObjectOptions struct {
	obj  ObjectRef
	opts []ObjectOption
}

func WithObjectOptions(obj client.Object, opts ...ObjectOption) *withObjectOptions {
	return &withObjectOptions{
		obj:  ToObjectRef(obj),
		opts: opts,
	}
}

// ApplyToPhaseOptions implements PhaseOption.
func (p *withObjectOptions) ApplyToPhaseOptions(opts *PhaseOptions) {
	opts.ObjectOptions[p.obj] = p.opts
}

type optionFn struct {
	fn func(opts *ObjectOptions)
}

// ApplyToObjectOptions implements ObjectOption.
func (p *optionFn) ApplyToObjectOptions(opts *ObjectOptions) {
	p.fn(opts)
}

// ApplyToPhaseOptions implements PhaseOption.
func (p *optionFn) ApplyToPhaseOptions(opts *PhaseOptions) {
	opts.DefaultObjectOptions = append(opts.DefaultObjectOptions, p)
}
