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

	// Allows creating objects in namespaces different to Owner.
	allowNamespaceEscalation bool
}

// NewClusterObjectValidator returns an ObjectValidator for cross-cluster deployments.
func NewClusterObjectValidator(
	restMapper restMapper,
	writer client.Writer,
) *ObjectValidator {
	return &ObjectValidator{
		restMapper: restMapper,
		writer:     writer,

		allowNamespaceEscalation: true,
	}
}

// NewNamespacedObjectValidator returns an ObjecctValidator for single-namespace deployments.
func NewNamespacedObjectValidator(
	restMapper restMapper,
	writer client.Writer,
) *ObjectValidator {
	return &ObjectValidator{
		restMapper: restMapper,
		writer:     writer,
	}
}

// Validate validates the given object and returns violations.
func (d *ObjectValidator) Validate(
	ctx context.Context, owner client.Object,
	obj *unstructured.Unstructured,
) (ObjectViolation, error) {
	// Static validation.
	if msgs := validateObjectMetadata(obj); len(msgs) > 0 {
		return newObjectViolation(obj, msgs), nil
	}

	if !d.allowNamespaceEscalation {
		// Ensure we are not leaving the namespace we are operating in.
		if vs := validateNamespace(d.restMapper, owner.GetNamespace(), obj); !vs.Empty() {
			return vs, nil
		}
	}

	// Dry run against API server to catch any other surprises.
	return validateDryRun(ctx, d.writer, obj), nil
}

func validateNamespace(
	restMapper restMapper,
	namespace string,
	obj *unstructured.Unstructured,
) *objectViolation {
	gvk := obj.GetObjectKind().GroupVersionKind()

	mapping, err := restMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if meta.IsNoMatchError(err) {
		// API does not exist in the cluster.
		return newObjectViolation(obj, []string{"not registered on the api server"})
	}

	// shortcut if Namespaces are not limited.
	if len(namespace) == 0 {
		return newObjectViolation(nil, nil)
	}

	switch mapping.Scope {
	case meta.RESTScopeRoot:
		return newObjectViolation(obj, []string{"object must be namespace-scoped"})

	case meta.RESTScopeNamespace:
		if obj.GetNamespace() == namespace {
			return newObjectViolation(obj, nil)
		}

		return newObjectViolation(obj, []string{fmt.Sprintf("object must belong to namespace %q", namespace)})
	}

	panic(fmt.Sprintf("unexpected REST Mapping Scope %q", mapping.Scope))
}

func validateDryRun(
	ctx context.Context,
	w client.Writer,
	obj *unstructured.Unstructured,
) *objectViolation {
	objectPatch, mErr := json.Marshal(obj)
	if mErr != nil {
		return newObjectViolation(obj, []string{fmt.Sprintf("creating patch: %s", mErr)})
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
		return newObjectViolation(nil, nil)

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
			return newObjectViolation(obj, []string{err.Error()})
		case "":
			logr.FromContextOrDiscard(ctx).Info("API status error with empty reason string", "err", apiErr.Status())

			if strings.Contains(
				apiErr.Status().Message,
				"failed to create typed patch object",
			) {
				return newObjectViolation(obj, []string{err.Error()})
			}
		}
	}

	return newObjectViolation(nil, nil)
}
