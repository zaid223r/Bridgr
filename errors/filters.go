package errors

import (
	"fmt"
	"strings"
)

type InvalidFilterFieldError struct {
	Fields []string
}

func (e *InvalidFilterFieldError) Error() string {
	if len(e.Fields) == 1 {
		return fmt.Sprintf("invalid filter field: %s", e.Fields[0])
	}
	return fmt.Sprintf("invalid filter fields: %s", strings.Join(e.Fields, ", "))
}
