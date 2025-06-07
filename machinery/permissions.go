package machinery

// import (
// 	rbacv1 "k8s.io/api/rbac/v1"
// 	"k8s.io/apimachinery/pkg/api/meta"
// 	"k8s.io/apimachinery/pkg/runtime/schema"
// 	"sigs.k8s.io/controller-runtime/pkg/client"

// 	"pkg.package-operator.run/boxcutter/machinery/types"
// )

// type restMapper interface {
// 	RESTMapping(gk schema.GroupKind, versions ...string) (
// 		*meta.RESTMapping, error)
// }

// // RBACAssembler collects all RBAC rules required for this package to function.
// type RBACAssembler struct {
// 	restMapper
// 	opts       RBACAssemblerOptions
// }

// type RBACAssemblerOptions struct {
// 	IgnoreMissingRestMapping bool
// 	// When UsingCache is true the verbs "list, watch" will be used instead of just "get".
// 	UsingCache bool
// }

// type RBACAssemblerOption interface {
// 	ApplyToRBACAssemblerOptions(opts *RBACAssemblerOptions)
// }

// func (ra *RBACAssembler) Revision(rev types.Revision, opts ...RBACAssemblerOption) []rbacv1.PolicyRule {
// 	return nil
// }

// func (ra *RBACAssembler) Phase(phase types.Phase, opts ...RBACAssemblerOption) []rbacv1.PolicyRule {
// 	return nil
// }

// func (ra *RBACAssembler) Object(obj client.Object, opts ...RBACAssemblerOption) []rbacv1.PolicyRule {
// 	return nil
// }
