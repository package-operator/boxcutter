package validation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	apimachineryerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type restMapper interface {
	RESTMapping(gk schema.GroupKind, versions ...string) (
		*meta.RESTMapping, error)
}

// ObjectValidator validates objects for structural,
// validation or permission scope issues.
type ObjectValidator struct {
	restMapper restMapper
	writer     client.Writer

	namespace string
}

// NewClusterObjectValidator returns an ObjectValidator for cross-cluster deployments.
func NewClusterObjectValidator(
	restMapper restMapper,
	writer client.Writer,
) *ObjectValidator {
	return NewNamespacedObjectValidator(restMapper, writer, "")
}

// NewNamespacedObjectValidator returns an ObjecctValidator for single-namespace deployments.
func NewNamespacedObjectValidator(
	restMapper restMapper,
	writer client.Writer,
	namespace string,
) *ObjectValidator {
	return &ObjectValidator{
		restMapper: restMapper,
		writer:     writer,
		namespace:  namespace,
	}
}

// Validate validates the given object and returns violations.
func (d *ObjectValidator) Validate(
	ctx context.Context, obj *unstructured.Unstructured,
) ([]Violation, error) {
	var violations []Violation
	defer addPositionToViolations(obj, &violations)

	// Static validation.
	if errs := validateObjectMetadata(obj); len(errs) > 0 {
		violations = []Violation{
			{Error: errs.ToAggregate().Error()},
		}
		return violations, nil
	}

	// Ensure we are not leaving the namespace we are operating in.
	if vs, err := validateNamespace(d.restMapper, d.namespace, obj); err != nil {
		return nil, err
	} else if len(vs) > 0 {
		return vs, nil
	}

	// Dry run against API server to catch any other surprises.
	return validateDryRun(ctx, d.writer, obj)
}

func validateNamespace(
	restMapper restMapper,
	namespace string,
	obj *unstructured.Unstructured,
) ([]Violation, error) {
	gvk := obj.GetObjectKind().GroupVersionKind()
	mapping, err := restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if meta.IsNoMatchError(err) {
		// API does not exist in the cluster.
		violations := []Violation{
			{Error: "not registered on the api server"},
		}
		return violations, nil
	}

	// shortcut if Namespaces are not limited.
	if len(namespace) == 0 {
		return nil, nil
	}

	switch mapping.Scope {
	case meta.RESTScopeRoot:
		violations := []Violation{
			{Error: "object must be namespace-scoped"},
		}
		return violations, nil

	case meta.RESTScopeNamespace:
		violations := []Violation{
			{Error: fmt.Sprintf("object must belong to namespace %q", namespace)},
		}
		return violations, nil
	}
	panic(fmt.Sprintf("unexpected REST Mapping Scope %q", mapping.Scope))
}

func validateDryRun(
	ctx context.Context,
	w client.Writer,
	obj *unstructured.Unstructured,
) ([]Violation, error) {
	objectPatch, mErr := json.Marshal(obj)
	if mErr != nil {
		return []Violation{{Error: fmt.Errorf("creating patch: %w", mErr).Error()}}, nil
	}

	patch := client.RawPatch(types.ApplyPatchType, objectPatch)
	dst := obj.DeepCopyObject().(*unstructured.Unstructured)
	err := w.Patch(ctx, dst, patch, client.FieldOwner("dummy"), client.ForceOwnership, client.DryRunAll)

	if apimachineryerrors.IsNotFound(err) {
		err = w.Create(ctx, obj.DeepCopyObject().(client.Object), client.DryRunAll)
	}

	var apiErr apimachineryerrors.APIStatus

	switch {
	case err == nil:
		return nil, nil

	case errors.As(err, &apiErr):
		switch apiErr.Status().Reason {
		case metav1.StatusReasonUnauthorized,
			metav1.StatusReasonForbidden,
			metav1.StatusReasonAlreadyExists,
			metav1.StatusReasonConflict,
			metav1.StatusReasonInvalid,
			metav1.StatusReasonBadRequest,
			metav1.StatusReasonMethodNotAllowed,
			metav1.StatusReasonRequestEntityTooLarge,
			metav1.StatusReasonUnsupportedMediaType,
			metav1.StatusReasonNotAcceptable,
			metav1.StatusReasonNotFound:
			return []Violation{{Error: err.Error()}}, nil
		case "":
			logr.FromContextOrDiscard(ctx).Info("API status error with empty reason string", "err", apiErr.Status())

			if strings.Contains(apiErr.Status().Message, "failed to create typed patch object") {
				return []Violation{{Error: err.Error()}}, nil
			}
		}
	}
	return nil, nil
}
