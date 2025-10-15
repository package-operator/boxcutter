package probing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestKindSelector(t *testing.T) {
	t.Parallel()

	t.Run("matches", func(t *testing.T) {
		t.Parallel()

		prober := &proberMock{}
		prober.
			On("Probe", mock.Anything).
			Return(Result{Status: StatusTrue})

		gk := schema.GroupKind{
			Kind: "Pod",
		}
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gk.WithVersion("v1"))
		s := &GroupKindSelector{
			Prober:    prober,
			GroupKind: gk,
		}
		r := s.Probe(obj)
		assert.Equal(t, StatusTrue, r.Status)
		assert.Empty(t, r.Messages)
		prober.AssertCalled(t, "Probe", mock.Anything)
	})

	t.Run("no match", func(t *testing.T) {
		t.Parallel()

		prober := &proberMock{}
		prober.
			On("Probe", mock.Anything).
			Return(true, "")

		gk := schema.GroupKind{
			Kind: "Pod",
		}
		obj := &unstructured.Unstructured{}
		s := &GroupKindSelector{
			Prober:    prober,
			GroupKind: gk,
		}
		r := s.Probe(obj)
		assert.Equal(t, StatusTrue, r.Status)
		assert.Empty(t, r.Messages)
		prober.AssertNotCalled(t, "Probe", mock.Anything)
	})
}

func TestLabelSelector(t *testing.T) {
	t.Parallel()

	t.Run("matches", func(t *testing.T) {
		t.Parallel()

		prober := &proberMock{}
		prober.
			On("Probe", mock.Anything).
			Return(Result{Status: StatusTrue})

		obj := &unstructured.Unstructured{}
		s := &LabelSelector{
			Prober:   prober,
			Selector: labels.Everything(),
		}
		r := s.Probe(obj)
		assert.Equal(t, StatusTrue, r.Status)
		assert.Empty(t, r.Messages)
		prober.AssertCalled(t, "Probe", mock.Anything)
	})

	t.Run("no match", func(t *testing.T) {
		t.Parallel()

		prober := &proberMock{}
		prober.
			On("Probe", mock.Anything).
			Return(Result{Status: StatusTrue})

		obj := &unstructured.Unstructured{}
		s := &LabelSelector{
			Prober:   prober,
			Selector: labels.Nothing(),
		}
		r := s.Probe(obj)
		assert.Equal(t, StatusTrue, r.Status)
		assert.Empty(t, r.Messages)
		prober.AssertNotCalled(t, "Probe", mock.Anything)
	})
}
