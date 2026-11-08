package brerrors

import (
	"fmt"
	"strings"
)

// InvalidFilterFieldError reports query parameters that are not filterable for a model.
// It is written to clients as 400 Bad Request.
type InvalidFilterFieldError struct {
	Fields []string
}

// Error lists the rejected fields.
func (e *InvalidFilterFieldError) Error() string {
	if len(e.Fields) == 1 {
		return fmt.Sprintf("invalid filter field: %s", e.Fields[0])
	}
	return fmt.Sprintf("invalid filter fields: %s", strings.Join(e.Fields, ", "))
}
