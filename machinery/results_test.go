package machinery

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"
	"sigs.k8s.io/structured-merge-diff/v6/typed"

	"pkg.package-operator.run/boxcutter/machinery/types"
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
	failedExampleProbe = map[string]types.Prober{
		types.ProgressProbeType: &probeStub{
			success: false,
			msgs:    []string{"broken: broken"},
		},
	}
)

func TestObjectResultCreated(t *testing.T) {
	t.Parallel()

	or := newObjectResultCreated(resultExampleObj, failedExampleProbe)
	assert.Equal(t, `Object Deployment.apps/v1 test/testi
Action: "Created"
Probes:
- Progress: Failed
  - broken: broken
`, or.String())
}

func TestNormalObjectResult(t *testing.T) {
	t.Parallel()

	or := newNormalObjectResult(
		ActionProgressed, resultExampleObj,
		CompareResult{
			ConflictingMangers: []CompareResultManagedFields{
				{
					Manager: "hans",
					Fields:  fieldpath.NewSet(fieldpath.MakePathOrDie("spec", "image")),
				},
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
Probes:
- Progress: Failed
  - broken: broken
Conflicts:
- "hans"
  .spec.image
Comparison:
- Modified:
  .spec.image
`, or.String())
}

type probeStub struct {
	success bool
	msgs    []string
}

func (s *probeStub) Probe(
	_ client.Object,
) (success bool, messages []string) {
	return s.success, s.msgs
}
