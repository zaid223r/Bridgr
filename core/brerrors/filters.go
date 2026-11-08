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

// InvalidFilterValueError reports a filter value that can't be converted to the
// field's type, e.g. ?done=maybe for a bool. It is written to clients as 400.
type InvalidFilterValueError struct {
	Field string
	Value string
	Type  string
}

// Error names the field, the rejected value and the expected type.
func (e *InvalidFilterValueError) Error() string {
	return fmt.Sprintf("invalid value %q for filter %s: want %s", e.Value, e.Field, e.Type)
}
