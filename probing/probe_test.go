package probing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ Prober = (*proberMock)(nil)

type proberMock struct {
	mock.Mock
}

func (m *proberMock) Probe(obj client.Object) Result {
	args := m.Called(obj)

	return args.Get(0).(Result)
}

func TestAnd(t *testing.T) {
	t.Parallel()
	t.Run("combines failure messages", func(t *testing.T) {
		t.Parallel()

		prober1 := &proberMock{}
		prober2 := &proberMock{}

		prober1.
			On("Probe", mock.Anything).
			Return(Result{Status: StatusFalse, Messages: []string{"error from prober1"}})
		prober2.
			On("Probe", mock.Anything).
			Return(Result{Status: StatusFalse, Messages: []string{"error from prober2"}})

		l := And{prober1, prober2}

		r := l.Probe(&unstructured.Unstructured{})
		assert.Equal(t, StatusFalse, r.Status)
		assert.Equal(t, []string{"error from prober1", "error from prober2"}, r.Messages)
	})

	t.Run("succeeds when all subprobes succeed", func(t *testing.T) {
		t.Parallel()

		prober1 := &proberMock{}
		prober2 := &proberMock{}

		prober1.
			On("Probe", mock.Anything).
			Return(Result{Status: StatusTrue, Messages: []string{"success from prober1"}})
		prober2.
			On("Probe", mock.Anything).
			Return(Result{Status: StatusTrue, Messages: []string{"success from prober2"}})

		l := And{prober1, prober2}

		r := l.Probe(&unstructured.Unstructured{})
		assert.Equal(t, StatusTrue, r.Status)
		assert.Equal(t, []string{"success from prober1", "success from prober2"}, r.Messages)
	})
}
