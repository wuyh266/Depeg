package domain

import "errors"

var (
	ErrInvalid      = errors.New("invalid input")
	ErrUnauthorized = errors.New("authentication required")
	ErrConflict     = errors.New("resource already exists")
	ErrNotFound     = errors.New("resource not found")
	ErrUnavailable  = errors.New("service unavailable")
)
