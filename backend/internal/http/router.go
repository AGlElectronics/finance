// Package http builds the chi router and ties handlers to dependencies.
package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"

	"github.com/AGlElectronics/finance/backend/internal/access"
	"github.com/AGlElectronics/finance/backend/internal/actor"
	"github.com/AGlElectronics/finance/backend/internal/audit"
	"github.com/AGlElectronics/finance/backend/internal/auth"
	"github.com/AGlElectronics/finance/backend/internal/authbase"
	"github.com/AGlElectronics/finance/backend/internal/catalog"
	"github.com/AGlElectronics/finance/backend/internal/clientip"
	"github.com/AGlElectronics/finance/backend/internal/config"
	"github.com/AGlElectronics/finance/backend/internal/denlink"
	"github.com/AGlElectronics/finance/backend/internal/docstore"
	"github.com/AGlElectronics/finance/backend/internal/service"
)

// Deps groups the runtime dependencies the HTTP layer needs.
type Deps struct {
	Config       *config.Config
	Pool         *pgxpool.Pool
	Logger       zerolog.Logger
	OIDCVerifier *auth.OIDCVerifier
	// Den is the pairing with Den; nil means a fresh standalone client.
	Den *denlink.Client
	// Store keeps issued PDFs; nil means S3 is not configured.
	Store docstore.Store
	// Clock replaces the time source (tests only).
	Clock func() time.Time
}

// NewRouter wires the chi router with middleware, CORS and routes.
func NewRouter(deps Deps, version string) http.Handler {
	r := chi.NewRouter()
	cfg := deps.Config

	r.Use(cfg.Auth.StripDevHeaders)
	r.Use(authbase.SecurityHeaders)
	r.Use(chimiddleware.RequestID)
	r.Use(clientip.Middleware(deps.Config.TrustedProxyCIDRs))
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Timeout(60 * time.Second))

	r.Use(hlog.NewHandler(deps.Logger))
	r.Use(hlog.AccessHandler(func(req *http.Request, status, size int, duration time.Duration) {
		hlog.FromRequest(req).Info().
			Str("method", req.Method).
			Str("path", req.URL.Path).
			Int("status", status).
			Int("size", size).
			Dur("duration", duration).
			Msg("http")
	}))
	r.Use(hlog.RequestIDHandler("request_id", "X-Request-ID"))

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: deps.Config.CORSAllowedOrigins,
		AllowedMethods: []string{"GET", "POST", "PATCH", "PUT", "DELETE", "OPTIONS"},
		// X-Dev-User and X-Dev-Groups only in dev mode.
		AllowedHeaders:   cfg.Auth.AllowedHeaders("Idempotency-Key"),
		ExposedHeaders:   []string{"X-Request-ID", "Idempotent-Replayed", "Content-Disposition"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	health := newHealthHandler(deps.Pool, version)
	r.Get("/livez", health.livez)
	r.Get("/readyz", health.readyz)

	// HR's service token or any staff sign-in (no Finance role needed).
	r.Get("/api/role-catalog", catalog.Handler(catalog.Self(cfg.RoleGroups), func(r *http.Request) bool {
		if catalog.ServiceToken(r, cfg.HRServiceToken) {
			return true
		}
		_, err := auth.Identify(r, cfg, deps.OIDCVerifier)
		return err == nil
	}))

	den := deps.Den
	if den == nil {
		dc := cfg.Den
		dc.Version = version
		den = denlink.New(deps.Pool, dc, deps.Logger)
	}
	r.Get("/api/brand", den.BrandHandler("/api/brand"))
	r.Get("/api/brand/{role}", den.AssetHandler)
	r.Get("/api/den/state", den.StateHandler)

	limiter := authbase.NewRateLimiter(authbase.RateLimitOptions{
		Rate: cfg.RateLimit, Burst: cfg.RateBurst,
		Key: authbase.BySubject(func(r *http.Request) (authbase.UserKey, bool) {
			a, ok := actor.FromContext(r.Context())
			if !ok {
				return authbase.UserKey{}, false
			}
			return authbase.UserKey{Issuer: a.Issuer, Subject: a.Subject}, true
		}),
	})

	auditWriter := audit.NewWriter(deps.Pool, deps.Logger)
	svc := service.New(deps.Pool, auditWriter, service.Options{
		Currency:               cfg.Currency,
		DefaultVATRateBp:       cfg.DefaultVATRateBp,
		InvoicePrefix:          cfg.InvoicePrefix,
		CreditNotePrefix:       cfg.CreditNotePrefix,
		ApprovalThresholdCents: cfg.ApprovalThresholdCents,
		MaxBackdateDays:        cfg.MaxBackdateDays,
		Store:                  deps.Store,
		Den:                    den,
	})
	if deps.Clock != nil {
		svc.SetClock(deps.Clock)
	}
	h := &handler{svc: svc}
	read := auth.Require(access.Read)
	write := auth.Require(access.Write)
	approve := auth.Require(access.Approve)
	admin := auth.Require(access.Admin)

	r.Route("/api", func(api chi.Router) {
		api.Use(den.RequirePaired)
		api.Use(auth.Middleware(deps.Config, deps.OIDCVerifier))
		api.Use(limiter.Middleware)

		api.Get("/me", meHandler(deps.Config))
		prefs := den.PreferencesHandler(func(r *http.Request) (string, string, bool) {
			a, _ := actor.FromContext(r.Context())
			if a == nil || a.Issuer == "" || a.Subject == "" {
				return "", "", false
			}
			return a.Issuer, a.Subject, true
		})
		api.Get("/me/preferences", prefs)
		api.Put("/me/preferences", prefs)

		api.Group(func(api chi.Router) {
			api.Use(auth.RequireAccess)

			api.With(read).Get("/dashboard", h.dashboard)

			api.Route("/customers", func(cr chi.Router) {
				cr.With(read).Get("/", h.listCustomers)
				cr.With(write).Post("/", h.createCustomer)
				cr.With(read).Get("/{id}", h.getCustomer)
				cr.With(write).Patch("/{id}", h.updateCustomer)
				cr.With(write).Delete("/{id}", h.deleteCustomer)
			})

			api.Route("/invoices", func(ir chi.Router) {
				ir.With(read).Get("/", h.listInvoices)
				ir.With(write).Post("/", h.createInvoice)
				ir.With(read).Get("/{id}", h.getInvoice)
				ir.With(read).Get("/{id}/pdf", h.invoicePDF)
				ir.With(write).Patch("/{id}", h.updateInvoice)
				ir.With(write).Delete("/{id}", h.deleteInvoice)
				ir.With(write).Post("/{id}/issue", h.issueInvoice)
				ir.With(write).Post("/{id}/void", h.voidInvoice)
				ir.With(write).Post("/{id}/payments", h.addPayment)
				ir.With(write).Post("/{id}/credit-notes", h.createCreditNote)
			})

			api.Route("/credit-notes", func(cr chi.Router) {
				cr.With(read).Get("/{id}", h.getCreditNote)
				cr.With(read).Get("/{id}/pdf", h.creditNotePDF)
				cr.With(write).Post("/{id}/refunds", h.createRefund)
			})

			api.Route("/expenses", func(er chi.Router) {
				er.With(read).Get("/", h.listExpenses)
				er.With(write).Post("/", h.createExpense)
				er.With(read).Get("/{id}", h.getExpense)
				er.With(write).Patch("/{id}", h.updateExpense)
				er.With(write).Delete("/{id}", h.deleteExpense)
			})

			api.With(read).Get("/ledger", h.ledger)

			api.Route("/approvals", func(ar chi.Router) {
				ar.With(read).Get("/", h.listApprovals)
				ar.With(read).Get("/{id}", h.getApproval)
				ar.With(approve).Post("/{id}/approve", h.approve)
				ar.Post("/{id}/reject", h.reject)
			})

			api.Route("/periods", func(pr chi.Router) {
				pr.With(read).Get("/", h.listPeriods)
				pr.With(approve).Post("/{month}/close", h.closePeriod)
				pr.With(admin).Post("/{month}/reopen", h.reopenPeriod)
			})

			api.With(read).Get("/settings", h.getSettings)
			api.With(admin).Put("/settings", h.updateSettings)

			api.With(auth.Require(access.Audit)).Get("/audit", h.auditLog)

			api.Route("/den", func(dr chi.Router) {
				dr.Use(admin)
				dr.Get("/", den.StatusHandler)
				dr.Post("/pair", audited(auditWriter, "den.paired", den.PairHandler))
				dr.Post("/sync", audited(auditWriter, "den.synced", den.SyncHandler))
				dr.Delete("/pair", audited(auditWriter, "den.unpaired", den.UnpairHandler))
			})
		})
	})

	return r
}

func meHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, _ := actor.FromContext(r.Context())
		out := map[string]any{"username": "", "auth_mode": cfg.Auth.AuthMode, "has_access": false,
			"groups": []string{}, "roles": []string{}, "permissions": []access.Permission{}}
		if a != nil {
			out["username"], out["has_access"] = a.Username, a.HasAccess
			out["issuer"], out["subject"] = a.Issuer, a.Subject
			if a.Groups != nil {
				out["groups"] = a.Groups
			}
			out["roles"] = a.Roles
			out["permissions"] = access.Permissions(a.Roles)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(out)
	}
}
