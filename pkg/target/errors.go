package target

import (
	"errors"
)

var (
	ErrCreatingMatcher = errors.New("unable to create matcher")
	ErrReviewFormat    = errors.New("unsupported request review")
	ErrRequestObject   = errors.New("invalid request object")
	ErrMatching        = errors.New("error matching the requested object")
	ErrCachingType     = errors.New("cannot cache non-namespace type")
)
