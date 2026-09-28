package util

import (
	"errors"
	"flag"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdmissionExportFlagDefaults(t *testing.T) {
	got := flag.CommandLine.Lookup("enable-admission-violation-export")
	require.NotNil(t, got)
	require.Equal(t, "false", got.DefValue)
	require.Nil(t, flag.CommandLine.Lookup("admission-channel"))
}

func TestAddPublishErrorBoundsDistinctClasses(t *testing.T) {
	errorsByClass := make(map[string]error)
	for i := 0; i < MaxConnectionStatusErrors+100; i++ {
		errorsByClass = AddPublishError(errorsByClass, fmt.Errorf("class-%03d: backend unavailable", i))
	}

	require.Len(t, errorsByClass, MaxConnectionStatusErrors)
	require.EqualError(t, errorsByClass[AdditionalPublishErrorsOmittedMessage], AdditionalPublishErrorsOmittedMessage)

	replacement := errors.New("class-000: replacement")
	errorsByClass = AddPublishError(errorsByClass, replacement)
	require.ErrorIs(t, errorsByClass["class-000"], replacement)
}
