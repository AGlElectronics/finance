package config

import (
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Currency != "EUR" || cfg.DefaultVATRateBp != 2000 || cfg.InvoicePrefix != "FIN-" ||
		cfg.CreditNotePrefix != "FIN-CN-" || cfg.ApprovalThresholdCents != 0 || cfg.MaxBackdateDays != 366 {
		t.Fatalf("defaults = %+v", cfg)
	}
	if g := cfg.RoleGroups["finance_bookkeeper"]; len(g) != 1 || g[0] != "finance_bookkeeper" {
		t.Fatalf("role groups = %v", cfg.RoleGroups)
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
	}
	for key, value := range cases {
		t.Run(key, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://x")
			t.Setenv(key, value)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("%s=%q: err = %v", key, value, err)
			}
		})
	}
}

func TestRoleGroupsFromEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("FINANCE_APPROVER_GROUPS", "treasurer, board")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if g := cfg.RoleGroups["finance_approver"]; len(g) != 2 || g[1] != "board" {
		t.Fatalf("approver groups = %v", g)
	}
}
