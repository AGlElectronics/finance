package config

import (
	"strings"
	"testing"
)

// devEnv is the smallest valid local configuration.
func devEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("APP_ENV", "development")
	t.Setenv("AUTH_MODE", "dev")
}

func TestLoadDefaults(t *testing.T) {
	devEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Currency != "EUR" || cfg.DefaultVATRateBp != 2000 || cfg.InvoicePrefix != "FIN-" ||
		cfg.CreditNotePrefix != "FIN-CN-" || cfg.ApprovalThresholdCents != 0 || cfg.MaxBackdateDays != 366 {
		t.Fatalf("defaults = %+v", cfg)
	}
	if g := cfg.RoleGroups["finance_bookkeeper"]; len(g) != 1 || g[0].Stored != "finance_bookkeeper" {
		t.Fatalf("role groups = %v", cfg.RoleGroups)
	}
	if cfg.OIDCAudience != "finance-api" || cfg.Groups.ClaimName() != "groups" || cfg.RateBurst != 60 {
		t.Fatalf("audience %q claim %q burst %d", cfg.OIDCAudience, cfg.Groups.ClaimName(), cfg.RateBurst)
	}
	if !cfg.DevAuth() {
		t.Fatal("AUTH_MODE=dev with APP_ENV=development is dev sign-in")
	}
}

// Without settings Finance runs in production with OIDC, and refuses to
// start until the OIDC issuer and the Den encryption key are set.
func TestSafeDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	_, err := Load()
	if err == nil {
		t.Fatal("production without OIDC_ISSUER_URL and DEN_LINK_ENCRYPTION_KEY must not start")
	}
	for _, want := range []string{"OIDC_ISSUER_URL", "DEN_LINK_ENCRYPTION_KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}
	t.Setenv("OIDC_ISSUER_URL", "https://auth.example.org/realms/conservices")
	t.Setenv("DEN_LINK_ENCRYPTION_KEY", "ZGV2LW9ubHktZmluYW5jZS1kZW4tbGluay1rZXktMDA=")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.AppEnv != "production" || cfg.DevAuth() {
		t.Fatalf("auth = %+v", cfg.Auth)
	}
}

func TestDevModeOnlyInDevelopment(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("AUTH_MODE", "dev")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "APP_ENV=development") {
		t.Fatalf("AUTH_MODE=dev outside development: %v", err)
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	cases := map[string]string{
		"FINANCE_CURRENCY":                 "euro",
		"FINANCE_DEFAULT_VAT_RATE_BP":      "20%",
		"FINANCE_INVOICE_PREFIX":           "FIN 2026 ",
		"FINANCE_CREDIT_NOTE_PREFIX":       "FIN-",
		"FINANCE_APPROVAL_THRESHOLD_CENTS": "-1",
		"FINANCE_MAX_BACKDATE_DAYS":        "99999",
		"AUTH_MODE":                        "none",
		"OIDC_GROUP_MATCH":                 "middle",
		"API_RATE_LIMIT":                   "lots",
		"DEN_LINK_ENCRYPTION_KEY":          "too-short",
	}
	for key, value := range cases {
		t.Run(key, func(t *testing.T) {
			devEnv(t)
			t.Setenv(key, value)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("%s=%q: err = %v", key, value, err)
			}
		})
	}
}

func TestRoleGroupsFromEnv(t *testing.T) {
	devEnv(t)
	t.Setenv("FINANCE_APPROVER_GROUPS", "/Treasurer, board")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	g := cfg.RoleGroups["finance_approver"]
	if len(g) != 2 || g[0].Stored != "treasurer" || g[0].Display != "/Treasurer" || g[1].Stored != "board" {
		t.Fatalf("approver groups = %v", g)
	}
}
