package machinery

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCreateCollisionError_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		msg  string
		want string
	}{
		{
			name: "simple error message",
			msg:  "object already exists",
			want: "object already exists",
		},
		{
			name: "detailed error message",
			msg:  "ConfigMap test/example already exists and is owned by another controller",
			want: "ConfigMap test/example already exists and is owned by another controller",
		},
		{
			name: "empty error message",
			msg:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := CreateCollisionError{msg: tt.msg}
			got := err.Error()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCreateCollisionError_Implementation(t *testing.T) {
	t.Parallel()

	err := CreateCollisionError{msg: "test error"}

	var _ error = err
}
