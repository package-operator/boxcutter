package machinery

import "errors"

// TeardownControllerChangedError is returned when
// teardown could not be run due to owner not being in control.
type TeardownControllerChangedError struct {
	msg string
}

// Error implements golangs error interface.
func (e TeardownControllerChangedError) Error() string {
	return e.msg
}

// TeardownRevisionError is returned when
// teardown could not be run due to the object belonging to a different revision.
type TeardownRevisionError struct {
	msg string
}

// Error implements golangs error interface.
func (e TeardownRevisionError) Error() string {
	return e.msg
}

// IsTeardownRejectedDueToOwnerOrRevisionChange returns true if the error
// indicates that object teardown is rejected due to a new revision or a different
// controller having taken over.
func IsTeardownRejectedDueToOwnerOrRevisionChange(err error) bool {
	var tcce TeardownControllerChangedError
	if errors.As(err, &tcce) {
		return true
	}

	var tre TeardownRevisionError
	return errors.As(err, &tre)
}

// CreateCollisionError is returned when boxcutter tries to create an object,
// but it already exists. \
// This happens when another actor has created the object and caches are slow,
// or the colliding object is excluded via cache selectors.
type CreateCollisionError struct {
	msg string
}

// Error implements golangs error interface.
func (e CreateCollisionError) Error() string {
	return e.msg
}
