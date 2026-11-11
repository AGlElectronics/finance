package auth

import (
	"errors"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/AGlElectronics/finance/backend/internal/access"
	"github.com/AGlElectronics/finance/backend/internal/actor"
	"github.com/AGlElectronics/finance/backend/internal/authbase"
	"github.com/AGlElectronics/finance/backend/internal/config"
	"github.com/AGlElectronics/finance/backend/internal/httperror"
)

// Middleware authenticates API requests.
//   - AUTH_MODE=dev (only with APP_ENV=development): X-Dev-User (or
//     DEV_DEFAULT_USER) stamps the actor and X-Dev-Groups (or
//     DEV_DEFAULT_GROUPS) its groups.
//   - otherwise: requires a valid Bearer access token from the shared
//     Keycloak whose aud contains OIDC_AUDIENCE.
//
// In both modes the groups map to Finance roles through the FINANCE_*_GROUPS
// settings and the shared matching rule.
func Middleware(cfg *config.Config, oidcVerifier *OIDCVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			a, err := Identify(r, cfg, oidcVerifier)
			if err != nil {
				httperror.Render(w, r, err)
				return
			}
			a.Roles = access.Resolve(cfg.Groups, a.Groups, cfg.RoleGroups)
			a.HasAccess = len(a.Roles) > 0
			next.ServeHTTP(w, r.WithContext(actor.With(ctx, a)))
		})
	}
}

// Identify works out who is calling: the dev headers in dev mode, else the
// verified bearer token. Roles are not resolved here.
func Identify(r *http.Request, cfg *config.Config, oidcVerifier *OIDCVerifier) (*actor.Actor, error) {
	if cfg.DevAuth() {
		name := strings.TrimSpace(r.Header.Get(authbase.HeaderDevUser))
		if name == "" {
			name = cfg.DevDefaultUser
		}
		if name == "" {
			name = "dev-finance-admin"
		}
		groups := cfg.DevDefaultGroups
		if raw := strings.TrimSpace(r.Header.Get(authbase.HeaderDevGroups)); raw != "" {
			groups = config.SplitCSV(raw)
		}
		return &actor.Actor{Username: name, Issuer: "dev:", Subject: name, Groups: groups}, nil
	}
	if oidcVerifier == nil {
		return nil, httperror.Internal(errors.New("oidc verifier not initialised"))
	}
	bearer := bearerFromHeader(r.Header.Get("Authorization"))
	if bearer == "" {
		return nil, httperror.Unauthorized("missing bearer token")
	}
	claims, err := oidcVerifier.Verify(r.Context(), bearer)
	if err != nil {
		log.Ctx(r.Context()).Debug().Err(err).Msg("finance oidc verify failed")
		return nil, httperror.Unauthorized("invalid token")
	}
	return &actor.Actor{
		Username: DisplayName(claims),
		Issuer:   claims.Issuer,
		Subject:  claims.Subject,
		Groups:   claims.Groups,
	}, nil
}

// RequireAccess rejects callers who are signed in but hold no Finance role.
func RequireAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a, ok := actor.FromContext(r.Context())
		if !ok || !a.HasAccess {
			httperror.Render(w, r, httperror.Forbidden("you don't have access to Finance"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Require rejects callers whose roles do not grant p.
func Require(p access.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			a, ok := actor.FromContext(r.Context())
			if !ok || !a.Can(p) {
				httperror.Render(w, r, httperror.Forbidden("this needs the permission "+string(p)))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// BearerFromHeader returns the token of an "Authorization: Bearer" header.
func BearerFromHeader(h string) string { return bearerFromHeader(h) }

func bearerFromHeader(h string) string {
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
