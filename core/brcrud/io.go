package brcrud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"

	"github.com/zaid223r/Bridgr/core/brcontext"
	"github.com/zaid223r/Bridgr/core/brerrors"
	"github.com/zaid223r/Bridgr/core/brschema"
)

// CreateInput is a custom request body for creating a T. Make one with Input.
type CreateInput[T any] struct {
	typ   reflect.Type
	build func(ctx context.Context, raw json.RawMessage) (T, error)
}

// Input makes the create endpoint (POST) read an In instead of a T and turn
// it into the record with fn:
//
//	Create: brcrud.Input(func(ctx context.Context, in CreateUser) (User, error) {
//		return User{Email: in.Email, PasswordHash: hash(in.Password)}, nil
//	}),
//
// The body is decoded into In; if In (or *In) has a Validate() error method, a
// failure responds 422. An error from fn is mapped like any other
// (brerrors.ValidationError → 422, ErrConflict → 409, …). The record fn
// returns then goes through BeforeCreate and Options.Validate as usual. In is
// documented in OpenAPI as the request body.
func Input[In, T any](fn func(ctx context.Context, in In) (T, error)) *CreateInput[T] {
	return &CreateInput[T]{
		typ: reflect.TypeFor[In](),
		build: func(ctx context.Context, raw json.RawMessage) (T, error) {
			in, err := decodeInput[In](raw)
			if err != nil {
				var zero T
				return zero, err
			}
			return fn(ctx, in)
		},
	}
}

// UpdateInput is a custom request body for replacing or updating a T. Make one
// with Apply.
type UpdateInput[T any] struct {
	typ   reflect.Type
	build func(ctx context.Context, current T, raw json.RawMessage) (T, error)
}

// Apply makes the replace and update endpoints (PUT and PATCH) read an In
// instead of a T, and compute the new record from the stored one with fn:
//
//	Update: brcrud.Apply(func(ctx context.Context, u User, in UpdateUser) (User, error) {
//		if in.Email != nil {
//			u.Email = *in.Email
//		}
//		return u, nil
//	}),
//
// PUT and PATCH behave the same: fn decides what changes, so use pointer
// fields in In to tell a missing field from a zero one. The body is decoded
// and validated as with Input; the record fn returns goes through
// BeforeUpdate and Options.Validate as usual. In is documented in OpenAPI as
// the request body of both.
func Apply[In, T any](fn func(ctx context.Context, current T, in In) (T, error)) *UpdateInput[T] {
	return &UpdateInput[T]{
		typ: reflect.TypeFor[In](),
		build: func(ctx context.Context, current T, raw json.RawMessage) (T, error) {
			in, err := decodeInput[In](raw)
			if err != nil {
				var zero T
				return zero, err
			}
			return fn(ctx, current, in)
		},
	}
}

// Response is a custom response body for a resource. Make one with Output.
type Response[T any] struct {
	typ  reflect.Type
	view func(T) any
}

// Output makes every endpoint of the resource respond with fn's result
// instead of the record itself; lists respond with an array of them:
//
//	Response: brcrud.Output(func(u User) UserOut {
//		return UserOut{ID: u.ID, Email: u.Email, Since: u.CreatedAt}
//	}),
//
// fn receives the stored record (writeonly fields already cleared) and must
// not fail. Out is documented in OpenAPI as every success response.
func Output[T, Out any](fn func(T) Out) *Response[T] {
	return &Response[T]{typ: reflect.TypeFor[Out](), view: func(v T) any { return fn(v) }}
}

func (c *CreateInput[T]) customType() reflect.Type { return c.typ }
func (u *UpdateInput[T]) customType() reflect.Type { return u.typ }
func (o *Response[T]) customType() reflect.Type    { return o.typ }

// inputError is a request body that decoded but failed In's Validate method.
type inputError struct{ err error }

func (e inputError) Error() string { return e.err.Error() }
func (e inputError) Unwrap() error { return e.err }

// bodyError is a body that isn't valid JSON for In.
type bodyError struct{ err error }

func (e bodyError) Error() string { return e.err.Error() }
func (e bodyError) Unwrap() error { return e.err }

func decodeInput[In any](raw json.RawMessage) (In, error) {
	var in In
	if err := json.Unmarshal(raw, &in); err != nil {
		return in, bodyError{err}
	}
	v, ok := any(&in).(interface{ Validate() error })
	if !ok {
		v, ok = any(in).(interface{ Validate() error })
	}
	if ok {
		if err := v.Validate(); err != nil {
			return in, inputError{err}
		}
	}
	return in, nil
}

// writeBuildError writes the error of a custom input: 400 for a bad body, 422
// for a failed Validate, otherwise the usual mapping.
func writeBuildError(w http.ResponseWriter, r *http.Request, err error) {
	if b, ok := errors.AsType[bodyError](err); ok {
		writeBodyError(w, r, b.err)
		return
	}
	if v, ok := errors.AsType[inputError](err); ok {
		writeValidation(w, r, v.err)
		return
	}
	brerrors.WriteError(w, r, err)
}

// writeBodyError writes a 400 for a malformed body, or 413 when it is too large.
func writeBodyError(w http.ResponseWriter, r *http.Request, err error) {
	p := brerrors.New(http.StatusBadRequest, "invalid JSON body: "+err.Error())
	if tooBig, ok := errors.AsType[*http.MaxBytesError](err); ok {
		p = brerrors.New(http.StatusRequestEntityTooLarge,
			fmt.Sprintf("request body is larger than %d bytes", tooBig.Limit))
	}
	p.Instance = r.URL.Path
	p.RequestID = brcontext.RequestID(r.Context())
	brerrors.Write(w, p)
}

// writeValidation writes a validation error as 422, with field errors when it
// is a brerrors.ValidationError.
func writeValidation(w http.ResponseWriter, r *http.Request, err error) {
	p := brerrors.FromError(r, err)
	if p.Status != http.StatusUnprocessableEntity {
		p = brerrors.New(http.StatusUnprocessableEntity, err.Error())
		p.Instance = r.URL.Path
		p.RequestID = brcontext.RequestID(r.Context())
	}
	brerrors.Write(w, p)
}

// checkResponseType rejects response types whose writeonly fields would be
// encoded.
func checkResponseType(where string, t reflect.Type) {
	if m, err := brschema.For(t); err == nil && len(m.WriteOnly) > 0 {
		panic(fmt.Sprintf("%s: Response type %s has writeonly fields; leave them out of the response type instead", where, m.Name))
	}
	if at, ok := brschema.NestedWriteOnly(t); ok {
		panic(fmt.Sprintf("%s: Response type: %s has writeonly fields, which would be encoded", where, at))
	}
}
