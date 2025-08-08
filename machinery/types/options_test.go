package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestRevisionReconcileOptions_ForPhase(t *testing.T) {
	t.Parallel()

	defaultOpts := []PhaseReconcileOption{
		WithCollisionProtection(CollisionProtectionPrevent),
	}

	phaseSpecificOpts := []PhaseReconcileOption{
		WithCollisionProtection(CollisionProtectionNone),
	}

	opts := RevisionReconcileOptions{
		DefaultPhaseOptions: defaultOpts,
		PhaseOptions: map[string][]PhaseReconcileOption{
			"test-phase": phaseSpecificOpts,
		},
	}

	t.Run("returns default options for unknown phase", func(t *testing.T) {
		t.Parallel()

		result := opts.ForPhase("unknown-phase")
		assert.Equal(t, defaultOpts, result)
	})

	t.Run("returns combined options for known phase", func(t *testing.T) {
		t.Parallel()

		result := opts.ForPhase("test-phase")
		assert.Len(t, result, 2)
		assert.Equal(t, defaultOpts[0], result[0])
		assert.Equal(t, phaseSpecificOpts[0], result[1])
	})

	t.Run("returns only default options when no phase-specific options", func(t *testing.T) {
		t.Parallel()

		opts := RevisionReconcileOptions{
			DefaultPhaseOptions: defaultOpts,
		}
		result := opts.ForPhase("any-phase")
		assert.Equal(t, defaultOpts, result)
	})
}

func TestRevisionTeardownOptions_ForPhase(t *testing.T) {
	t.Parallel()

	defaultOpts := []PhaseTeardownOption{}
	phaseSpecificOpts := []PhaseTeardownOption{}

	opts := RevisionTeardownOptions{
		DefaultPhaseOptions: defaultOpts,
		PhaseOptions: map[string][]PhaseTeardownOption{
			"test-phase": phaseSpecificOpts,
		},
	}

	t.Run("returns default options for unknown phase", func(t *testing.T) {
		t.Parallel()

		result := opts.ForPhase("unknown-phase")
		assert.Equal(t, defaultOpts, result)
	})

	t.Run("returns combined options for known phase", func(t *testing.T) {
		t.Parallel()

		result := opts.ForPhase("test-phase")
		assert.Equal(t, append(defaultOpts, phaseSpecificOpts...), result)
	})
}

func TestPhaseReconcileOptions_ForObject(t *testing.T) {
	t.Parallel()

	obj := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: "test-ns",
		},
	}

	objRef := ToObjectRef(obj)

	defaultOpts := []ObjectReconcileOption{
		WithCollisionProtection(CollisionProtectionPrevent),
	}

	objectSpecificOpts := []ObjectReconcileOption{
		WithCollisionProtection(CollisionProtectionNone),
	}

	opts := PhaseReconcileOptions{
		DefaultObjectOptions: defaultOpts,
		ObjectOptions: map[ObjectRef][]ObjectReconcileOption{
			objRef: objectSpecificOpts,
		},
	}

	t.Run("returns default options for unknown object", func(t *testing.T) {
		t.Parallel()

		unknownObj := &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Secret",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "unknown-secret",
				Namespace: "test-ns",
			},
		}
		result := opts.ForObject(unknownObj)
		assert.Equal(t, defaultOpts, result)
	})

	t.Run("returns combined options for known object", func(t *testing.T) {
		t.Parallel()

		result := opts.ForObject(obj)
		assert.Len(t, result, 2)
		assert.Equal(t, defaultOpts[0], result[0])
		assert.Equal(t, objectSpecificOpts[0], result[1])
	})
}

func TestPhaseTeardownOptions_ForObject(t *testing.T) {
	t.Parallel()

	obj := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: "test-ns",
		},
	}

	objRef := ToObjectRef(obj)

	defaultOpts := []ObjectTeardownOption{}
	objectSpecificOpts := []ObjectTeardownOption{}

	opts := PhaseTeardownOptions{
		DefaultObjectOptions: defaultOpts,
		ObjectOptions: map[ObjectRef][]ObjectTeardownOption{
			objRef: objectSpecificOpts,
		},
	}

	t.Run("returns default options for unknown object", func(t *testing.T) {
		t.Parallel()

		unknownObj := &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Secret",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "unknown-secret",
				Namespace: "test-ns",
			},
		}
		result := opts.ForObject(unknownObj)
		assert.Equal(t, defaultOpts, result)
	})

	t.Run("returns combined options for known object", func(t *testing.T) {
		t.Parallel()

		result := opts.ForObject(obj)
		assert.Equal(t, append(defaultOpts, objectSpecificOpts...), result)
	})
}

func TestObjectReconcileOptions_Default(t *testing.T) {
	t.Parallel()

	t.Run("sets default collision protection when empty", func(t *testing.T) {
		t.Parallel()

		opts := &ObjectReconcileOptions{}
		opts.Default()
		assert.Equal(t, CollisionProtectionPrevent, opts.CollisionProtection)
	})

	t.Run("preserves existing collision protection", func(t *testing.T) {
		t.Parallel()

		opts := &ObjectReconcileOptions{
			CollisionProtection: CollisionProtectionNone,
		}
		opts.Default()
		assert.Equal(t, CollisionProtectionNone, opts.CollisionProtection)
	})
}

func TestObjectTeardownOptions_Default(t *testing.T) {
	t.Parallel()

	opts := &ObjectTeardownOptions{}
	opts.Default()
}

func TestWithCollisionProtection(t *testing.T) {
	t.Parallel()

	protection := WithCollisionProtection(CollisionProtectionNone)

	t.Run("applies to object reconcile options", func(t *testing.T) {
		t.Parallel()

		opts := &ObjectReconcileOptions{}
		protection.ApplyToObjectReconcileOptions(opts)
		assert.Equal(t, CollisionProtectionNone, opts.CollisionProtection)
	})

	t.Run("applies to phase reconcile options", func(t *testing.T) {
		t.Parallel()

		opts := &PhaseReconcileOptions{}
		protection.ApplyToPhaseReconcileOptions(opts)
		require.Len(t, opts.DefaultObjectOptions, 1)
		assert.Equal(t, protection, opts.DefaultObjectOptions[0])
	})

	t.Run("applies to revision reconcile options", func(t *testing.T) {
		t.Parallel()

		opts := &RevisionReconcileOptions{}
		protection.ApplyToRevisionReconcileOptions(opts)
		require.Len(t, opts.DefaultPhaseOptions, 1)
		assert.Equal(t, protection, opts.DefaultPhaseOptions[0])
	})
}

func TestWithPreviousOwners(t *testing.T) {
	t.Parallel()

	owner1 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "owner1",
			Namespace: "test",
		},
	}
	owner2 := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "owner2",
			Namespace: "test",
		},
	}

	previousOwners := WithPreviousOwners{owner1, owner2}

	t.Run("applies to object reconcile options", func(t *testing.T) {
		t.Parallel()

		opts := &ObjectReconcileOptions{}
		previousOwners.ApplyToObjectReconcileOptions(opts)
		assert.Equal(t, []client.Object{owner1, owner2}, opts.PreviousOwners)
	})

	t.Run("applies to phase reconcile options", func(t *testing.T) {
		t.Parallel()

		opts := &PhaseReconcileOptions{}
		previousOwners.ApplyToPhaseReconcileOptions(opts)
		require.Len(t, opts.DefaultObjectOptions, 1)
		assert.Equal(t, previousOwners, opts.DefaultObjectOptions[0])
	})

	t.Run("applies to revision reconcile options", func(t *testing.T) {
		t.Parallel()

		opts := &RevisionReconcileOptions{}
		previousOwners.ApplyToRevisionReconcileOptions(opts)
		require.Len(t, opts.DefaultPhaseOptions, 1)
		assert.Equal(t, previousOwners, opts.DefaultPhaseOptions[0])
	})
}

func TestWithPaused(t *testing.T) {
	t.Parallel()

	paused := WithPaused{}

	t.Run("applies to object reconcile options", func(t *testing.T) {
		t.Parallel()

		opts := &ObjectReconcileOptions{}
		paused.ApplyToObjectReconcileOptions(opts)
		assert.True(t, opts.Paused)
	})

	t.Run("applies to phase reconcile options", func(t *testing.T) {
		t.Parallel()

		opts := &PhaseReconcileOptions{}
		paused.ApplyToPhaseReconcileOptions(opts)
		require.Len(t, opts.DefaultObjectOptions, 1)
		assert.Equal(t, paused, opts.DefaultObjectOptions[0])
	})

	t.Run("applies to revision reconcile options", func(t *testing.T) {
		t.Parallel()

		opts := &RevisionReconcileOptions{}
		paused.ApplyToRevisionReconcileOptions(opts)
		require.Len(t, opts.DefaultPhaseOptions, 1)
		assert.Equal(t, paused, opts.DefaultPhaseOptions[0])
	})
}

func TestProbeFunc(t *testing.T) {
	t.Parallel()

	obj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: "test-ns",
		},
	}

	t.Run("wraps function correctly", func(t *testing.T) {
		t.Parallel()

		expectedSuccess := true
		expectedMessages := []string{"test message"}

		probeFn := ProbeFunc(func(_ client.Object) (bool, []string) {
			return expectedSuccess, expectedMessages
		})

		success, messages := probeFn.Probe(obj)
		assert.Equal(t, expectedSuccess, success)
		assert.Equal(t, expectedMessages, messages)
	})
}

func TestWithProbe(t *testing.T) {
	t.Parallel()

	probe := ProbeFunc(func(_ client.Object) (bool, []string) {
		return true, []string{"success"}
	})

	probeOption := WithProbe("test-probe", probe)

	t.Run("applies probe to object reconcile options", func(t *testing.T) {
		t.Parallel()

		opts := &ObjectReconcileOptions{}
		probeOption.ApplyToObjectReconcileOptions(opts)

		require.NotNil(t, opts.Probes)
		assert.Contains(t, opts.Probes, "test-probe")
		assert.Equal(t, probe, opts.Probes["test-probe"])
	})

	t.Run("preserves existing probes", func(t *testing.T) {
		t.Parallel()

		existingProbe := ProbeFunc(func(_ client.Object) (bool, []string) {
			return false, []string{"existing"}
		})

		opts := &ObjectReconcileOptions{
			Probes: map[string]Prober{
				"existing-probe": existingProbe,
			},
		}

		probeOption.ApplyToObjectReconcileOptions(opts)

		require.Len(t, opts.Probes, 2)
		assert.Equal(t, existingProbe, opts.Probes["existing-probe"])
		assert.Equal(t, probe, opts.Probes["test-probe"])
	})
}

func TestWithObjectReconcileOptions(t *testing.T) {
	t.Parallel()

	obj := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: "test-ns",
		},
	}

	objectOpts := []ObjectReconcileOption{
		WithCollisionProtection(CollisionProtectionNone),
	}

	withObjOpts := WithObjectReconcileOptions(obj, objectOpts...)

	t.Run("applies to phase reconcile options", func(t *testing.T) {
		t.Parallel()

		opts := &PhaseReconcileOptions{}
		withObjOpts.ApplyToPhaseReconcileOptions(opts)

		objRef := ToObjectRef(obj)

		require.NotNil(t, opts.ObjectOptions)
		assert.Contains(t, opts.ObjectOptions, objRef)
		assert.Equal(t, objectOpts, opts.ObjectOptions[objRef])
	})

	t.Run("applies to revision reconcile options", func(t *testing.T) {
		t.Parallel()

		opts := &RevisionReconcileOptions{}
		withObjOpts.ApplyToRevisionReconcileOptions(opts)
		require.Len(t, opts.DefaultPhaseOptions, 1)
	})
}

func TestWithObjectTeardownOptions(t *testing.T) {
	t.Parallel()

	obj := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: "test-ns",
		},
	}

	objectOpts := []ObjectTeardownOption{}

	withObjOpts := WithObjectTeardownOptions(obj, objectOpts...)

	t.Run("applies to phase teardown options", func(t *testing.T) {
		t.Parallel()

		opts := &PhaseTeardownOptions{}
		withObjOpts.ApplyToPhaseTeardownOptions(opts)

		objRef := ToObjectRef(obj)

		require.NotNil(t, opts.ObjectOptions)
		assert.Contains(t, opts.ObjectOptions, objRef)
		assert.Equal(t, objectOpts, opts.ObjectOptions[objRef])
	})

	t.Run("applies to revision teardown options", func(t *testing.T) {
		t.Parallel()

		opts := &RevisionTeardownOptions{}
		withObjOpts.ApplyToRevisionTeardownOptions(opts)
		require.Len(t, opts.DefaultPhaseOptions, 1)
	})
}

func TestWithPhaseReconcileOptions(t *testing.T) {
	t.Parallel()

	phaseOpts := []PhaseReconcileOption{
		WithCollisionProtection(CollisionProtectionNone),
	}

	withPhaseOpts := WithPhaseReconcileOptions("test-phase", phaseOpts...)

	t.Run("applies to revision reconcile options", func(t *testing.T) {
		t.Parallel()

		opts := &RevisionReconcileOptions{}
		withPhaseOpts.ApplyToRevisionReconcileOptions(opts)

		require.NotNil(t, opts.PhaseOptions)
		assert.Contains(t, opts.PhaseOptions, "test-phase")
		assert.Equal(t, phaseOpts, opts.PhaseOptions["test-phase"])
	})
}

func TestWithPhaseTeardownOptions(t *testing.T) {
	t.Parallel()

	phaseOpts := []PhaseTeardownOption{}

	withPhaseOpts := WithPhaseTeardownOptions("test-phase", phaseOpts...)

	t.Run("applies to revision teardown options", func(t *testing.T) {
		t.Parallel()

		opts := &RevisionTeardownOptions{}
		withPhaseOpts.ApplyToRevisionTeardownOptions(opts)

		require.NotNil(t, opts.PhaseOptions)
		assert.Contains(t, opts.PhaseOptions, "test-phase")
		assert.Equal(t, phaseOpts, opts.PhaseOptions["test-phase"])
	})
}

func TestCollisionProtectionConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, CollisionProtectionPrevent, CollisionProtection("Prevent"))
	assert.Equal(t, CollisionProtectionIfNoController, CollisionProtection("IfNoController"))
	assert.Equal(t, CollisionProtectionNone, CollisionProtection("None"))
}

func TestProgressProbeType(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Progress", ProgressProbeType)
}

func TestInterfaceImplementations(t *testing.T) {
	t.Parallel()

	var _ ObjectReconcileOption = WithCollisionProtection("")

	var _ ObjectReconcileOption = WithPaused{}

	var _ ObjectReconcileOption = WithPreviousOwners{}

	var _ PhaseReconcileOption = WithCollisionProtection("")

	var _ PhaseReconcileOption = WithPaused{}

	var _ PhaseReconcileOption = WithPreviousOwners{}

	var _ RevisionReconcileOption = WithCollisionProtection("")

	var _ RevisionReconcileOption = WithPaused{}

	var _ RevisionReconcileOption = WithPreviousOwners{}
}
