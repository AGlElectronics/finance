package auth

import (
	"errors"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/AGlElectronics/finance/backend/internal/access"
	"github.com/AGlElectronics/finance/backend/internal/actor"
	"github.com/AGlElectronics/finance/backend/internal/config"
	"github.com/AGlElectronics/finance/backend/internal/httperror"
)

// Middleware authenticates API requests.
//   - AUTH_MODE=dev: X-Dev-User (or DEV_DEFAULT_USER) stamps the actor and
//     X-Dev-Groups (or DEV_DEFAULT_GROUPS) its groups.
//   - AUTH_MODE=oidc: requires a valid Bearer ID token from the shared Keycloak.
//
// In both modes the groups map to Finance roles through the FINANCE_*_GROUPS
// settings.
func Middleware(cfg *config.Config, oidcVerifier *OIDCVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			var a *actor.Actor

			switch cfg.AuthMode {
			case config.AuthModeDev, "":
				name := strings.TrimSpace(r.Header.Get("X-Dev-User"))
				if name == "" {
					name = cfg.DevDefaultUser
				}
				if name == "" {
					name = "dev-finance-admin"
				}
				groups := cfg.DevDefaultGroups
				if raw := strings.TrimSpace(r.Header.Get("X-Dev-Groups")); raw != "" {
					groups = config.SplitCSV(raw)
				}
				a = &actor.Actor{Username: name, Issuer: "dev:", Subject: name, Groups: groups}

			case config.AuthModeOIDC:
				if oidcVerifier == nil {
					httperror.Render(w, r, httperror.Internal(errors.New("oidc verifier not initialised")))
					return
				}
				bearer := bearerFromHeader(r.Header.Get("Authorization"))
				if bearer == "" {
					httperror.Render(w, r, httperror.Unauthorized("missing bearer token"))
					return
				}
				claims, err := oidcVerifier.Verify(ctx, bearer)
				if err != nil {
					log.Ctx(ctx).Debug().Err(err).Msg("finance oidc verify failed")
					httperror.Render(w, r, httperror.Unauthorized("invalid token"))
					return
				}
				a = &actor.Actor{
					Username: DisplayName(claims),
					Issuer:   claims.Issuer,
					Subject:  claims.Subject,
					Groups:   claims.Groups,
				}

			default:
				httperror.Render(w, r, httperror.Internal(errors.New("unknown auth mode")))
				return
			}

			a.Roles = access.Resolve(a.Groups, cfg.RoleGroups)
			a.HasAccess = len(a.Roles) > 0
			next.ServeHTTP(w, r.WithContext(actor.With(ctx, a)))
		})
	}
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
