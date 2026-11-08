// Package brcrud registers REST CRUD routes for a model type.
package brcrud

// BridgrModel is the storage contract the CRUD handlers call. Filters are the raw
// query-string values of the list request.
//
// Get, Update and Delete return an error wrapping brerrors.ErrNotFound when no
// record has the id; List returns *brerrors.InvalidFilterFieldError for filters the
// model doesn't allow. Other errors are treated as internal (500).
type BridgrModel[T any] interface {
	List(filters map[string][]string) ([]T, error)
	Get(id string) (T, error)
	Create(input T) (T, error)
	Update(id string, input T) (T, error)
	Delete(id string) error
}
