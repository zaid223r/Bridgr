package brcap

import (
	"context"
	"slices"
)

// Principal is the authenticated caller of a request. An auth module stores it
// in the request context with WithPrincipal; rate limiting, auditing and
// authorization read it with PrincipalFrom.
type Principal struct {
	// ID identifies the caller, e.g. a user ID or an API key ID.
	ID string
	// Roles are the caller's roles or scopes.
	Roles []string
	// Attrs holds other claims, such as a tenant ID.
	Attrs map[string]string
}

// HasRole reports whether the principal has role.
func (p Principal) HasRole(role string) bool {
	return slices.Contains(p.Roles, role)
}

type principalKey struct{}

// WithPrincipal returns a copy of ctx carrying p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the principal stored by WithPrincipal, if any.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}
