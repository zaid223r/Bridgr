// Package errors defines the error types Bridgr returns to clients.
package errors

import (
	"fmt"
	"strings"
)

// InvalidFilterFieldError reports query parameters that are not filterable for a model.
type InvalidFilterFieldError struct {
	Fields []string
}

func (e *InvalidFilterFieldError) Error() string {
	if len(e.Fields) == 1 {
		return fmt.Sprintf("invalid filter field: %s", e.Fields[0])
	}
	return fmt.Sprintf("invalid filter fields: %s", strings.Join(e.Fields, ", "))
}
