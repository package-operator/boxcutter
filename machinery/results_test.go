package machinery

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/structured-merge-diff/v4/fieldpath"
	"sigs.k8s.io/structured-merge-diff/v4/typed"
)

var (
	resultExampleObj = &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      "testi",
				"namespace": "test",
			},
		},
	}
	failedExampleProbe = &probeStub{
		success: false,
		msgs:    []string{"broken: broken"},
	}
)

func TestObjectResultCreated(t *testing.T) {
	t.Parallel()
	or := newObjectResultCreated(resultExampleObj, failedExampleProbe)
	assert.Equal(t, `Object Deployment.apps/v1 test/testi
Action: "Created"
Probe:  Failed
- broken: broken
`, or.String())
}

func TestNormalObjectResult(t *testing.T) {
	t.Parallel()
	or := newNormalObjectResult(
		ActionProgressed, resultExampleObj,
		DivergeResult{
			ConflictingFieldOwners: []string{"hans"},
			ConflictingPathsByFieldOwner: map[string]*fieldpath.Set{
				"hans": fieldpath.NewSet(fieldpath.MakePathOrDie("spec", "image")),
			},
			Comparison: &typed.Comparison{
				Modified: fieldpath.NewSet(
					fieldpath.MakePathOrDie("spec", "image"),
				),
				Removed: fieldpath.NewSet(),
			},
		}, failedExampleProbe)

	assert.Equal(t, `Object Deployment.apps/v1 test/testi
Action: "Progressed"
Probe:  Failed
- broken: broken
Updated:
- .spec.image
Conflicting Field Managers: hans
Fields contested by "hans":
- .spec.image
`, or.String())
}

type probeStub struct {
	success bool
	msgs    []string
}

func (s *probeStub) Probe(
	_ *unstructured.Unstructured,
) (success bool, messages []string) {
	return s.success, s.msgs
}
