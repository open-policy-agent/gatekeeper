package policy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExitError(t *testing.T) {
	tests := []struct {
		name         string
		createErr    func() *ExitError
		expectedCode int
		expectedMsg  string
	}{
		{
			name: "general error",
			createErr: func() *ExitError {
				return NewGeneralError("something went wrong")
			},
			expectedCode: ExitGeneralError,
			expectedMsg:  "something went wrong",
		},
		{
			name: "cluster error",
			createErr: func() *ExitError {
				return NewClusterError("cluster not found")
			},
			expectedCode: ExitClusterError,
			expectedMsg:  "cluster not found",
		},
		{
			name: "conflict error",
			createErr: func() *ExitError {
				return NewConflictError("resource exists")
			},
			expectedCode: ExitConflictError,
			expectedMsg:  "resource exists",
		},
		{
			name: "partial success error",
			createErr: func() *ExitError {
				return NewPartialSuccessError("2 of 5 installed")
			},
			expectedCode: ExitPartialSuccess,
			expectedMsg:  "2 of 5 installed",
		},
		{
			name: "custom exit error",
			createErr: func() *ExitError {
				return NewExitError(42, "custom error")
			},
			expectedCode: 42,
			expectedMsg:  "custom error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.createErr()
			assert.Equal(t, tt.expectedCode, err.Code)
			assert.Equal(t, tt.expectedMsg, err.Message)
			assert.Equal(t, tt.expectedMsg, err.Error())
		})
	}
}

func TestExitCodes(t *testing.T) {
	assert.Equal(t, 0, ExitSuccess)
	assert.Equal(t, 1, ExitGeneralError)
	assert.Equal(t, 2, ExitClusterError)
	assert.Equal(t, 3, ExitConflictError)
	assert.Equal(t, 4, ExitPartialSuccess)
}
