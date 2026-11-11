// Package config loads the Finance server configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"github.com/AGlElectronics/finance/backend/internal/access"
	"github.com/AGlElectronics/finance/backend/internal/clientip"
	"github.com/AGlElectronics/finance/backend/internal/denlink"
)

const (
	AuthModeDev  = "dev"
	AuthModeOIDC = "oidc"
)

type Config struct {
	AppEnv      string
	AppBaseURL  string
	HTTPAddr    string
	DatabaseURL string

	// S3 stores the PDF of every issued invoice and credit note. Issuing
	// fails with 503 storage_unavailable while S3 is not configured.
	S3Endpoint        string
	S3Bucket          string
	S3AccessKeyID     string
	S3SecretAccessKey string
	S3Region          string
	S3UsePathStyle    bool

	AuthMode       string
	DevDefaultUser string
	// DevDefaultGroups are the groups of a dev-mode caller that sends no
	// X-Dev-Groups header (DEV_DEFAULT_GROUPS).
	DevDefaultGroups []string

	OIDCIssuerURL string
	OIDCClientID  string
	OIDCAudience  string
	// RoleGroups maps each Finance role (access.Role*) to the IdP groups that
	// grant it (FINANCE_ADMIN_GROUPS, FINANCE_APPROVER_GROUPS,
	// FINANCE_BOOKKEEPER_GROUPS, FINANCE_READONLY_GROUPS).
	RoleGroups map[string][]string

	// Currency is the one currency of this installation (FINANCE_CURRENCY).
	Currency string
	// DefaultVATRateBp pre-fills new invoice lines (FINANCE_DEFAULT_VAT_RATE_BP).
	DefaultVATRateBp int32
	// InvoicePrefix and CreditNotePrefix start the document numbers
	// (FIN-0001, FIN-CN-0001).
	InvoicePrefix    string
	CreditNotePrefix string
	// ApprovalThresholdCents: voids, payments, expenses, credit notes and
	// refunds above this amount need a second person (0 = all of them).
	ApprovalThresholdCents int64
	// MaxBackdateDays is how far in the past a document date may lie.
	MaxBackdateDays int

	// DenInternalURL replaces the host of pasted Den pairing URLs, for when
	// Den is reachable under a different address from this backend (Docker).
	DenInternalURL string
	// DenMode is DEN_MODE: required keeps the API locked until the module
	// is paired with Den; standalone (the default) does not.
	DenMode denlink.Mode

	CORSAllowedOrigins []string
	// TrustedProxyCIDRs lists the proxies whose X-Forwarded-For entries are
	// believed when working out the client IP (TRUSTED_PROXY_CIDRS).
	TrustedProxyCIDRs []netip.Prefix

	ShutdownTimeout time.Duration
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		AppEnv:            getEnv("APP_ENV", "development"),
		AppBaseURL:        getEnv("APP_BASE_URL", "http://localhost:8080"),
		HTTPAddr:          getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL:       getEnv("DATABASE_URL", ""),
		S3Endpoint:        getEnv("S3_ENDPOINT", ""),
		S3Bucket:          getEnv("S3_BUCKET", "finance-data-dev"),
		S3AccessKeyID:     getEnv("S3_ACCESS_KEY_ID", ""),
		S3SecretAccessKey: getEnv("S3_SECRET_ACCESS_KEY", ""),
		S3Region:          getEnv("S3_REGION", "us-east-1"),
		S3UsePathStyle:    getEnvBool("S3_USE_PATH_STYLE", true),
		AuthMode:          strings.ToLower(getEnv("AUTH_MODE", AuthModeDev)),
		DevDefaultUser:    getEnv("DEV_DEFAULT_USER", "dev-finance-admin"),
		DevDefaultGroups:  SplitCSV(getEnv("DEV_DEFAULT_GROUPS", access.RoleAdmin)),
		OIDCIssuerURL:     getEnv("OIDC_ISSUER_URL", ""),
		OIDCClientID:      getEnv("OIDC_CLIENT_ID", ""),
		OIDCAudience:      getEnv("OIDC_AUDIENCE", ""),
		RoleGroups: map[string][]string{
			access.RoleAdmin:      SplitCSV(getEnv("FINANCE_ADMIN_GROUPS", access.RoleAdmin)),
			access.RoleApprover:   SplitCSV(getEnv("FINANCE_APPROVER_GROUPS", access.RoleApprover)),
			access.RoleBookkeeper: SplitCSV(getEnv("FINANCE_BOOKKEEPER_GROUPS", access.RoleBookkeeper)),
			access.RoleReadOnly:   SplitCSV(getEnv("FINANCE_READONLY_GROUPS", access.RoleReadOnly)),
		},
		Currency:           strings.ToUpper(getEnv("FINANCE_CURRENCY", "EUR")),
		InvoicePrefix:      getEnv("FINANCE_INVOICE_PREFIX", "FIN-"),
		CreditNotePrefix:   getEnv("FINANCE_CREDIT_NOTE_PREFIX", "FIN-CN-"),
		DenInternalURL:     getEnv("DEN_INTERNAL_URL", ""),
		DenMode:            denlink.Mode(getEnv("DEN_MODE", string(denlink.ModeStandalone))),
		CORSAllowedOrigins: SplitCSV(getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3006")),
		ShutdownTimeout:    getEnvDuration("SHUTDOWN_TIMEOUT", 15*time.Second),
	}

	var errs []string
	if v, err := getEnvInt("FINANCE_DEFAULT_VAT_RATE_BP", 2000); err != nil {
		errs = append(errs, err.Error())
	} else {
		cfg.DefaultVATRateBp = int32(v)
	}
	if v, err := getEnvInt("FINANCE_APPROVAL_THRESHOLD_CENTS", 0); err != nil {
		errs = append(errs, err.Error())
	} else {
		cfg.ApprovalThresholdCents = v
	}
	if v, err := getEnvInt("FINANCE_MAX_BACKDATE_DAYS", 366); err != nil {
		errs = append(errs, err.Error())
	} else {
		cfg.MaxBackdateDays = int(v)
	}
	if len(errs) > 0 {
		return nil, errors.New("invalid config: " + strings.Join(errs, "; "))
	}

	trusted, err := clientip.ParseCIDRs(getEnv("TRUSTED_PROXY_CIDRS", clientip.DefaultTrustedProxies))
	if err != nil {
		return nil, fmt.Errorf("invalid config: TRUSTED_PROXY_CIDRS: %w", err)
	}
	cfg.TrustedProxyCIDRs = trusted

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

var (
	currencyRE = regexp.MustCompile(`^[A-Z]{3}$`)
	prefixRE   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9/_-]{0,15}$`)
)

func (c *Config) validate() error {
	var errs []string
	if !currencyRE.MatchString(c.Currency) {
		errs = append(errs, "FINANCE_CURRENCY must be a three-letter ISO 4217 code such as EUR")
	}
	if c.DefaultVATRateBp < 0 || c.DefaultVATRateBp > 10000 {
		errs = append(errs, "FINANCE_DEFAULT_VAT_RATE_BP must be between 0 and 10000")
	}
	if !prefixRE.MatchString(c.InvoicePrefix) {
		errs = append(errs, "FINANCE_INVOICE_PREFIX must be 1-16 letters, digits, '-', '_' or '/'")
	}
	if !prefixRE.MatchString(c.CreditNotePrefix) {
		errs = append(errs, "FINANCE_CREDIT_NOTE_PREFIX must be 1-16 letters, digits, '-', '_' or '/'")
	}
	if c.InvoicePrefix == c.CreditNotePrefix {
		errs = append(errs, "FINANCE_INVOICE_PREFIX and FINANCE_CREDIT_NOTE_PREFIX must differ")
	}
	if c.ApprovalThresholdCents < 0 {
		errs = append(errs, "FINANCE_APPROVAL_THRESHOLD_CENTS must be 0 or more")
	}
	if c.MaxBackdateDays < 0 || c.MaxBackdateDays > 3660 {
		errs = append(errs, "FINANCE_MAX_BACKDATE_DAYS must be between 0 and 3660")
	}
	for role, groups := range c.RoleGroups {
		if len(groups) == 0 {
			errs = append(errs, "the groups of role "+role+" must not be empty")
		}
	}
	if c.DatabaseURL == "" {
		errs = append(errs, "DATABASE_URL is required")
	}
	if err := c.DenMode.Validate(); err != nil {
		errs = append(errs, err.Error())
	}
	switch c.AuthMode {
	case AuthModeDev, AuthModeOIDC:
	default:
		errs = append(errs, "AUTH_MODE must be 'dev' or 'oidc'")
	}
	if c.AuthMode == AuthModeOIDC {
		if c.OIDCIssuerURL == "" {
			errs = append(errs, "OIDC_ISSUER_URL is required when AUTH_MODE=oidc")
		}
		if c.OIDCClientID == "" {
			errs = append(errs, "OIDC_CLIENT_ID is required when AUTH_MODE=oidc")
		}
	}
	s3Any := c.S3Endpoint != "" || c.S3AccessKeyID != "" || c.S3SecretAccessKey != ""
	if s3Any {
		if c.S3Endpoint == "" {
			errs = append(errs, "S3_ENDPOINT is required when any S3_* var is set")
		}
		if c.S3Bucket == "" {
			errs = append(errs, "S3_BUCKET is required when any S3_* var is set")
		}
		if c.S3AccessKeyID == "" {
			errs = append(errs, "S3_ACCESS_KEY_ID is required when any S3_* var is set")
		}
		if c.S3SecretAccessKey == "" {
			errs = append(errs, "S3_SECRET_ACCESS_KEY is required when any S3_* var is set")
		}
	}
	if len(errs) > 0 {
		return errors.New("invalid config: " + strings.Join(errs, "; "))
	}
	return nil
}

func (c *Config) S3Configured() bool {
	return c.S3Endpoint != "" && c.S3Bucket != "" && c.S3AccessKeyID != "" && c.S3SecretAccessKey != ""
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvInt(key string, fallback int64) (int64, error) {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a whole number, got %q", key, v)
	}
	return parsed, nil
}

// DefaultRoleGroups maps every role to the group of the same name, which is
// also what the role catalog publishes.
func DefaultRoleGroups() map[string][]string {
	out := map[string][]string{}
	for _, r := range access.Roles {
		out[r] = []string{r}
	}
	return out
}

// SplitCSV splits a comma-separated list, dropping blanks.
func SplitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (c *Config) String() string {
	return fmt.Sprintf(
		"env=%s addr=%s auth=%s db=%s bucket=%s cors=%v",
		c.AppEnv, c.HTTPAddr, c.AuthMode, maskDSN(c.DatabaseURL), c.S3Bucket, c.CORSAllowedOrigins,
	)
}

func maskDSN(dsn string) string {
	if dsn == "" {
		return ""
	}
	at := strings.LastIndex(dsn, "@")
	colon := strings.Index(dsn, "://")
	if at > 0 && colon > 0 && at > colon+3 {
		return dsn[:colon+3] + "***:***" + dsn[at:]
	}
	return dsn
}
