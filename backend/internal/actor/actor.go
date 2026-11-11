// Package actor identifies who performed a Finance write (audit / created_by).
// In AUTH_MODE=dev this comes from X-Dev-User; in oidc from verified claims.
package actor

import (
	"context"

	"github.com/AGlElectronics/finance/backend/internal/access"
	"github.com/AGlElectronics/finance/backend/internal/audit"
)

type ctxKey int

const key ctxKey = 1

// Actor is the authenticated caller.
type Actor struct {
	Username string
	// Issuer and Subject identify the person at the IdP (Den keys shared
	// preferences on them). Dev mode uses issuer "dev:" like Den.
	Issuer, Subject string
	Groups          []string
	// Roles are the Finance roles the caller's groups grant (access.Role*).
	Roles []string
	// HasAccess is true when the caller holds at least one Finance role.
	HasAccess bool
}

// Can reports whether the actor's roles grant p.
func (a *Actor) Can(p access.Permission) bool {
	return a != nil && access.Has(a.Roles, p)
}

// Audit is the actor as recorded in audit events.
func (a *Actor) Audit() audit.Actor {
	if a == nil {
		return audit.Actor{Name: "unknown"}
	}
	return audit.Actor{Name: a.Username, Issuer: a.Issuer, Subject: a.Subject}
}

// With attaches an Actor to ctx.
func With(ctx context.Context, a *Actor) context.Context {
	return context.WithValue(ctx, key, a)
}

// FromContext returns the Actor if middleware attached one.
func FromContext(ctx context.Context) (*Actor, bool) {
	a, ok := ctx.Value(key).(*Actor)
	if !ok || a == nil || a.Username == "" {
		return nil, false
	}
	return a, true
}

// Name returns Username or fallback.
func Name(ctx context.Context, fallback string) string {
	if a, ok := FromContext(ctx); ok {
		return a.Username
	}
	return fallback
}
