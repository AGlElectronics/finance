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

	"github.com/AGlElectronics/finance/backend/internal/actor"
	"github.com/AGlElectronics/finance/backend/internal/audit"
	"github.com/AGlElectronics/finance/backend/internal/auth"
	"github.com/AGlElectronics/finance/backend/internal/catalog"
	"github.com/AGlElectronics/finance/backend/internal/config"
	"github.com/AGlElectronics/finance/backend/internal/denlink"
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
}

// NewRouter wires the chi router with middleware, CORS and routes.
func NewRouter(deps Deps, version string) http.Handler {
	r := chi.NewRouter()

	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
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
		AllowedOrigins:   deps.Config.CORSAllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PATCH", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Content-Type", "Authorization", "X-Dev-User", "X-Request-ID"},
		ExposedHeaders:   []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	health := newHealthHandler(deps.Pool, version)
	r.Get("/livez", health.livez)
	r.Get("/readyz", health.readyz)

	r.Get("/api/role-catalog", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, catalog.Self())
	})

	den := deps.Den
	if den == nil {
		den = denlink.New(deps.Pool, denlink.Config{Slug: "finance", Version: version}, deps.Logger)
	}
	r.Get("/api/brand", den.BrandHandler("/api/brand"))
	r.Get("/api/brand/{role}", den.AssetHandler)

	auditWriter := audit.NewWriter(deps.Pool, deps.Logger)
	customerSvc := service.NewCustomerService(deps.Pool, auditWriter)
	invoiceSvc := service.NewInvoiceService(deps.Pool, auditWriter)
	dashSvc := service.NewDashboardService(deps.Pool, invoiceSvc)
	expenseSvc := service.NewExpenseService(deps.Pool, auditWriter)
	ledgerSvc := service.NewLedgerService(deps.Pool)

	customers := &customerHandler{svc: customerSvc}
	invoices := &invoiceHandler{svc: invoiceSvc, dash: dashSvc}
	expenses := &expenseHandler{svc: expenseSvc, ledger: ledgerSvc}

	r.Route("/api", func(api chi.Router) {
		api.Use(auth.Middleware(deps.Config, deps.OIDCVerifier))

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

			api.Get("/dashboard", invoices.dashboard)

			api.Route("/customers", func(cr chi.Router) {
				cr.Get("/", customers.list)
				cr.Post("/", customers.create)
				cr.Get("/{id}", customers.get)
				cr.Patch("/{id}", customers.update)
				cr.Delete("/{id}", customers.remove)
			})

			api.Route("/invoices", func(ir chi.Router) {
				ir.Get("/", invoices.list)
				ir.Post("/", invoices.create)
				ir.Get("/{id}", invoices.get)
				ir.Patch("/{id}", invoices.update)
				ir.Delete("/{id}", invoices.remove)
				ir.Post("/{id}/issue", invoices.issue)
				ir.Post("/{id}/void", invoices.void)
				ir.Post("/{id}/payments", invoices.addPayment)
			})

			api.Route("/expenses", func(er chi.Router) {
				er.Get("/", expenses.list)
				er.Post("/", expenses.create)
				er.Get("/{id}", expenses.get)
				er.Patch("/{id}", expenses.update)
				er.Delete("/{id}", expenses.remove)
			})

			api.Get("/ledger", expenses.ledgerGet)

			api.Route("/den", func(dr chi.Router) {
				dr.Get("/", den.StatusHandler)
				dr.Post("/pair", den.PairHandler)
				dr.Post("/sync", den.SyncHandler)
				dr.Delete("/pair", den.UnpairHandler)
			})
		})
	})

	return r
}

func meHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, _ := actor.FromContext(r.Context())
		out := map[string]any{"username": "", "auth_mode": cfg.AuthMode, "has_access": false, "groups": []string{}}
		if a != nil {
			out["username"], out["has_access"] = a.Username, a.HasAccess
			if a.Groups != nil {
				out["groups"] = a.Groups
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(out)
	}
}
