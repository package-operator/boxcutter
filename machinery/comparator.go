package machinery

import (
	"bytes"
	"fmt"
	"slices"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/openapi"
	"k8s.io/client-go/openapi3"
	"k8s.io/kube-openapi/pkg/schemaconv"
	"k8s.io/kube-openapi/pkg/spec3"
	"k8s.io/kube-openapi/pkg/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/structured-merge-diff/v4/fieldpath"
	"sigs.k8s.io/structured-merge-diff/v4/typed"
)

// Comparator detects divergent state between desired and actual
// by comparing managed field ownerships.
// If not all fields from desired are owned by the same field owner in actual,
// we know that the object has been updated by another actor.
type Comparator struct {
	ownerStrategy   divergeDetectorOwnerStrategy
	openAPIAccessor openAPIAccessor
	fieldOwner      string
}

type discoveryClient interface {
	OpenAPIV3() openapi.Client
}

type divergeDetectorOwnerStrategy interface {
	SetControllerReference(owner, obj metav1.Object) error
}

type openAPIAccessor interface {
	Get(gv schema.GroupVersion) (*spec3.OpenAPI, error)
}

// NewComparator returns a new Comparator instance.
func NewComparator(
	ownerStrategy divergeDetectorOwnerStrategy,
	discoveryClient discoveryClient,
	fieldOwner string,
) *Comparator {
	return &Comparator{
		ownerStrategy: ownerStrategy,
		openAPIAccessor: &defaultOpenAPIAccessor{
			c: discoveryClient.OpenAPIV3(),
		},
		fieldOwner: fieldOwner,
	}
}

type defaultOpenAPIAccessor struct {
	c openapi.Client
}

func (a *defaultOpenAPIAccessor) Get(gv schema.GroupVersion) (*spec3.OpenAPI, error) {
	r := openapi3.NewRoot(a.c)
	return r.GVSpec(gv)
}

// DivergeResult holds the results of a diverge check.
type DivergeResult struct {
	// List of other conflicting field owners.
	ConflictingFieldOwners []string
	// Mapping of field owner name to conflicting fieldsets.
	ConflictingPathsByFieldOwner map[string]*fieldpath.Set
	// Comparison of desired fields to actual fields.
	Comparison *typed.Comparison
}

// IsConflict returns true, if another actor has overidden changes.
func (d DivergeResult) IsConflict() bool {
	return len(d.ConflictingFieldOwners) > 0
}

// ConflictingPaths returns a list if conflicting field paths indexed by their owner.
func (d DivergeResult) ConflictingPaths() map[string][]string {
	if d.ConflictingPathsByFieldOwner == nil {
		return nil
	}
	out := map[string][]string{}
	for k, v := range d.ConflictingPathsByFieldOwner {
		v.Iterate(func(p fieldpath.Path) {
			out[k] = append(out[k], p.String())
		})
	}
	return out
}

// Modified returns a list of fields that have been modified.
func (d DivergeResult) Modified() []string {
	if d.Comparison == nil {
		return nil
	}
	var out []string
	d.Comparison.Modified.Iterate(func(p fieldpath.Path) {
		out = append(out, p.String())
	})
	d.Comparison.Removed.Iterate(func(p fieldpath.Path) {
		out = append(out, p.String())
	})
	return out
}

// HasDiverged checks if a resource has been changed from desired.
func (d *Comparator) HasDiverged(
	owner client.Object,
	desiredObject, actualObject *unstructured.Unstructured,
) (res DivergeResult, err error) {
	gvk := desiredObject.GroupVersionKind()
	if gvk != actualObject.GroupVersionKind() {
		panic("desired and actual must have same GVK")
	}

	// Get OpenAPISchema to have the correct merge and field configuration.
	s, err := d.openAPIAccessor.Get(gvk.GroupVersion())
	if err != nil {
		return res, fmt.Errorf("API accessor: %w", err)
	}
	ss, err := schemaconv.ToSchemaFromOpenAPI(s.Components.Schemas, false)
	if err != nil {
		return res, fmt.Errorf("schema from OpenAPI: %w", err)
	}

	var parser typed.Parser
	ss.CopyInto(&parser.Schema)

	// Get "our" managed fields on actual.
	mf, ok := findManagedFields(d.fieldOwner, actualObject)
	if !ok {
		// not a single managed field from "us" -> diverged for sure
		// -> diverged on EVERYTHING.
		// TODO: sort out how to report this, because listing ALL fields is not helpful.
		res.ConflictingPathsByFieldOwner = map[string]*fieldpath.Set{}
		for _, mf := range actualObject.GetManagedFields() {
			res.ConflictingFieldOwners = append(res.ConflictingFieldOwners, mf.Manager)
			res.ConflictingPathsByFieldOwner[mf.Manager] = &fieldpath.Set{}
		}
		return res, nil
	}
	actualFieldSet := &fieldpath.Set{}
	if err := actualFieldSet.FromJSON(bytes.NewReader(mf.FieldsV1.Raw)); err != nil {
		return res, fmt.Errorf("field set for actual: %w", err)
	}

	// Extrapolate a field set from desired.
	desiredObject = desiredObject.DeepCopy()
	if err := d.ownerStrategy.SetControllerReference(owner, desiredObject); err != nil {
		return res, err
	}
	tName, err := openAPICanonicalName(*desiredObject)
	if err != nil {
		return res, err
	}
	typedDesired, err := parser.Type(tName).FromUnstructured(desiredObject.Object)
	if err != nil {
		return res, fmt.Errorf("struct merge type conversion: %w", err)
	}

	desiredFieldSet, err := typedDesired.ToFieldSet()
	if err != nil {
		return res, fmt.Errorf("desired to field set: %w", err)
	}

	// Diff field sets to get exclude all ownership references
	// that are the same between actual and desired.
	// Also limit results to leave nodes to keep resulting diff small.
	diff := desiredFieldSet.Difference(actualFieldSet).Difference(stripSet).Leaves()

	// Index diff into something more useful for the caller.
	managerPaths := map[string]*fieldpath.Set{}
	for _, mf := range actualObject.GetManagedFields() {
		fs := &fieldpath.Set{}
		if err := fs.FromJSON(bytes.NewReader(mf.FieldsV1.Raw)); err != nil {
			return res, fmt.Errorf("field set for actual: %w", err)
		}
		diff.Iterate(func(p fieldpath.Path) {
			if !fs.Has(p) {
				return
			}
			if _, ok := managerPaths[mf.Manager]; !ok {
				managerPaths[mf.Manager] = &fieldpath.Set{}
			}
			managerPaths[mf.Manager].Insert(p)
		})
	}
	for fieldOwner := range managerPaths {
		res.ConflictingFieldOwners = append(res.ConflictingFieldOwners, fieldOwner)
	}
	slices.Sort(res.ConflictingFieldOwners)
	if len(managerPaths) > 0 {
		res.ConflictingPathsByFieldOwner = managerPaths
	}

	typedActual, err := parser.Type(tName).FromUnstructured(actualObject.Object)
	if err != nil {
		return res, fmt.Errorf("from unstructured: %w", err)
	}
	actualValues := typedActual.ExtractItems(desiredFieldSet)
	m := actualValues.AsValue().Unstructured().(map[string]interface{})
	stripNils(m)
	actualValues, err = parser.Type(tName).FromUnstructured(m)
	if err != nil {
		return res, fmt.Errorf("struct merge type conversion: %w", err)
	}
	res.Comparison, err = typedDesired.Compare(actualValues)
	if err != nil {
		return res, fmt.Errorf("compare: %w", err)
	}
	return res, nil
}

// Returns the ManagedFields associated with the given field owner.
func findManagedFields(fieldOwner string, accessor metav1.Object) (metav1.ManagedFieldsEntry, bool) {
	objManagedFields := accessor.GetManagedFields()
	for _, mf := range objManagedFields {
		if mf.Manager == fieldOwner &&
			mf.Operation == metav1.ManagedFieldsOperationApply &&
			mf.Subresource == "" {
			return mf, true
		}
	}
	return metav1.ManagedFieldsEntry{}, false
}

// taken from:
// https://github.com/kubernetes/apimachinery/blob/v0.32.0-alpha.0/pkg/util/managedfields/internal/stripmeta.go#L39-L52
var stripSet = fieldpath.NewSet(
	fieldpath.MakePathOrDie("apiVersion"),
	fieldpath.MakePathOrDie("kind"),
	fieldpath.MakePathOrDie("metadata"),
	fieldpath.MakePathOrDie("metadata", "name"),
	fieldpath.MakePathOrDie("metadata", "namespace"),
	fieldpath.MakePathOrDie("metadata", "creationTimestamp"),
	fieldpath.MakePathOrDie("metadata", "selfLink"),
	fieldpath.MakePathOrDie("metadata", "uid"),
	fieldpath.MakePathOrDie("metadata", "clusterName"),
	fieldpath.MakePathOrDie("metadata", "generation"),
	fieldpath.MakePathOrDie("metadata", "managedFields"),
	fieldpath.MakePathOrDie("metadata", "resourceVersion"),
)

var existingAPIScheme = runtime.NewScheme()

func init() {
	schemeBuilder := runtime.SchemeBuilder{
		scheme.AddToScheme,
		apiextensionsv1.AddToScheme,
		apiextensions.AddToScheme,
	}
	if err := schemeBuilder.AddToScheme(existingAPIScheme); err != nil {
		panic(err)
	}
}

// Returns the canonical name to find the OpenAPISchema for the given objects GVK.
func openAPICanonicalName(obj unstructured.Unstructured) (string, error) {
	gvk := obj.GroupVersionKind()

	var schemaTypeName string
	o, err := existingAPIScheme.New(gvk)
	switch {
	case err != nil && runtime.IsNotRegisteredError(err):
		// Assume CRD, when GVK is not part of core APIs.
		schemaTypeName = fmt.Sprintf("%s/%s.%s", gvk.Group, gvk.Version, gvk.Kind)
	case err != nil:
		return "", err
	default:
		schemaTypeName = util.GetCanonicalTypeName(o)
	}
	return util.ToRESTFriendlyName(schemaTypeName), nil
}

func stripNils(o map[string]interface{}) {
	for k, v := range o {
		if v == nil {
			// o[k] = map[interface{}]interface{}{}
			delete(o, k)
			continue
		}
		if m, ok := v.(map[string]interface{}); ok {
			stripNils(m)
		}

		l, ok := v.([]interface{})
		if !ok {
			continue
		}
		var newList []interface{}
		for _, v := range l {
			if v == nil {
				continue
			}
			if m, ok := v.(map[string]interface{}); ok {
				stripNils(m)
			}
			newList = append(newList, v)
		}
		o[k] = newList
	}
}
