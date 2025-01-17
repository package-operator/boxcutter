package types

import "sigs.k8s.io/controller-runtime/pkg/client"

type PhaseStandin struct {
	Name    string
	Objects []PhaseObject
}

func (p *PhaseStandin) GetName() string {
	return p.Name
}

func (p *PhaseStandin) GetObjects() []PhaseObject {
	return p.Objects
}

type RevisionStandin struct {
	Name     string
	Owner    client.Object
	Revision int64
	Phases   []Phase
}

func (r *RevisionStandin) GetName() string {
	return r.Name
}

func (r *RevisionStandin) GetClientObject() client.Object {
	return r.Owner
}

func (r *RevisionStandin) GetRevisionNumber() int64 {
	return r.Revision
}

func (r *RevisionStandin) GetPhases() []Phase {
	return r.Phases
}
