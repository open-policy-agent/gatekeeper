package assignimage

import "fmt"

type baseError struct {
	s string
}

func (e baseError) Error() string {
	return e.s
}

// Component field (domain|path|tag) errors.
type invalidDomainError struct{ baseError }

type (
	invalidPathError       struct{ baseError }
	invalidTagError        struct{ baseError }
	missingComponentsError struct{ baseError }
	domainLikePathError    struct{ baseError }
)

// Location field errors.
type (
	listTerminalError struct{ baseError }
	metadataRootError struct{ baseError }
)

func newInvalidDomainError(domain string) invalidDomainError {
	return invalidDomainError{
		baseError{
			fmt.Sprintf("assignDomain %q must be a fully-qualified domain name or localhost", domain),
		},
	}
}

func newInvalidPathError(path string) invalidPathError {
	return invalidPathError{
		baseError{
			fmt.Sprintf("assignPath %q must be a valid docker image path", path),
		},
	}
}

func newInvalidTagError(tag string) invalidTagError {
	return invalidTagError{
		baseError{
			fmt.Sprintf("assignTag %q must be a valid docker image tag or digest starting with ':' or '@'", tag),
		},
	}
}

func newMissingComponentsError() missingComponentsError {
	return missingComponentsError{
		baseError{
			"at least one of [assignDomain, assignPath, assignTag] must be set",
		},
	}
}

func newDomainLikePathError(path string) domainLikePathError {
	return domainLikePathError{
		baseError{
			fmt.Sprintf("assignDomain must be set if the first part of assignPath %q can be interpretted as part of a domain", path),
		},
	}
}

func newListTerminalError(name string) listTerminalError {
	return listTerminalError{
		baseError{
			fmt.Sprintf("assignImage %s cannot mutate list-type fields", name),
		},
	}
}

func newMetadataRootError(name string) metadataRootError {
	return metadataRootError{
		baseError{
			fmt.Sprintf("assignImage %s can't change metadata", name),
		},
	}
}
