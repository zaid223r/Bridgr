package bridgr

import (
	"context"

	"github.com/zaid223r/Bridgr/core/brcrud"
)

// Input sets a custom create body for a resource: the endpoint reads an In and
// fn turns it into the record. Use it in CRUDOptions.Create:
//
//	Create: bridgr.Input(func(ctx context.Context, in CreateUser) (User, error) {
//		return User{Email: in.Email, PasswordHash: hash(in.Password)}, nil
//	}),
//
// See brcrud.Input for validation and error handling.
func Input[In, T any](fn func(ctx context.Context, in In) (T, error)) *brcrud.CreateInput[T] {
	return brcrud.Input(fn)
}

// Apply sets a custom replace and update body (PUT and PATCH): the endpoints
// read an In and fn computes the new record from the stored one. Use it in
// CRUDOptions.Update; see brcrud.Apply.
func Apply[In, T any](fn func(ctx context.Context, current T, in In) (T, error)) *brcrud.UpdateInput[T] {
	return brcrud.Apply(fn)
}

// Output sets a custom response body: every endpoint of the resource responds
// with fn's result instead of the record. Use it in CRUDOptions.Response; see
// brcrud.Output.
func Output[T, Out any](fn func(T) Out) *brcrud.Response[T] {
	return brcrud.Output(fn)
}
