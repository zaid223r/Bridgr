package brerrors

import "strings"

// FieldError is one problem with one field of a request body.
type FieldError struct {
	// Field is the JSON name of the field, e.g. "title" or "address.city".
	Field string `json:"field"`
	// Message says what's wrong, e.g. "is required".
	Message string `json:"message"`
}

// ValidationError reports field-level problems with a request body. Returned
// from a Validate function (or by a validation module) it becomes a 422
// response whose "errors" list has one entry per field.
type ValidationError struct {
	Fields []FieldError
}

// Error lists every field problem: "title: is required; priority: must be 1-3".
func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		parts[i] = f.Field + ": " + f.Message
	}
	return strings.Join(parts, "; ")
}

// Add appends a field problem.
func (e *ValidationError) Add(field, message string) {
	e.Fields = append(e.Fields, FieldError{Field: field, Message: message})
}

// OrNil returns e as an error if it has any field problems, otherwise nil, so a
// validator can end with `return errs.OrNil()`.
func (e *ValidationError) OrNil() error {
	if e == nil || len(e.Fields) == 0 {
		return nil
	}
	return e
}
