// Package placeholder defines the explicit failure used by unfinished adapters.
package placeholder

import "errors"

// TODO: Replace each use with a real implementation and a domain-specific error.
var ErrNotImplemented = errors.New("depeg: not implemented (project skeleton)")
