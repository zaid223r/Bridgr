// Package brcrud registers REST CRUD routes for a model type.
package brcrud

import "context"

// BridgrModel is the storage contract the CRUD handlers call. ctx is the request
// context: it is cancelled when the client goes away or the server shuts down, so
// pass it to the database. Filters are the raw query-string values of the list
// request.
//
// Get, Update and Delete return an error wrapping brerrors.ErrNotFound when no
// record has the id; List returns *brerrors.InvalidFilterFieldError for filters the
// model doesn't allow. Other errors are treated as internal (500).
type BridgrModel[T any] interface {
	List(ctx context.Context, filters map[string][]string) ([]T, error)
	Get(ctx context.Context, id string) (T, error)
	Create(ctx context.Context, input T) (T, error)
	Update(ctx context.Context, id string, input T) (T, error)
	Delete(ctx context.Context, id string) error
}
