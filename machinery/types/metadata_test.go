package types

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// mockRevisionMetadata is a test mock for RevisionMetadata.
// Not tested directly here, but used in other tests.
type mockRevisionMetadata struct {
	name string
}

func (m *mockRevisionMetadata) GetReconcileOptions() []RevisionReconcileOption      { return nil }
func (m *mockRevisionMetadata) GetTeardownOptions() []RevisionTeardownOption        { return nil }
func (m *mockRevisionMetadata) SetCurrent(metav1.Object, ...SetCurrentOption) error { return nil }
func (m *mockRevisionMetadata) IsCurrent(metav1.Object) bool                        { return false }
func (m *mockRevisionMetadata) RemoveFrom(metav1.Object)                            {}
func (m *mockRevisionMetadata) IsNamespaceAllowed(metav1.Object) bool               { return true }
func (m *mockRevisionMetadata) CopyReferences(metav1.Object, metav1.Object)         {}
func (m *mockRevisionMetadata) GetCurrent(metav1.Object) RevisionReference          { return nil }

var _ RevisionMetadata = &mockRevisionMetadata{}
