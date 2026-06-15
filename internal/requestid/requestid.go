// Package requestid provides context storage for HTTP correlation IDs.
// This file is located at /internal/requestid/requestid.go.
//
// It is intentionally minimal so that any package (adapter, middleware,
// handler) can read the request ID from a context without importing the
// HTTP middleware layer and triggering import cycles.
package requestid

import "context"

// key is an unexported context key type scoped to this package.
type key struct{}

// FromContext retrieves the request ID stored by the middleware layer.
// Returns an empty string if none is present.
func FromContext(ctx context.Context) string {
	id, _ := ctx.Value(key{}).(string)
	return id
}

// NewContext returns a copy of ctx carrying the given request ID.
func NewContext(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, key{}, id)
}
