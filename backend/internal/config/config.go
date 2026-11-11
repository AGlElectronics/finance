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
	"github.com/AGlElectronics/finance/backend/internal/authbase"
	"github.com/AGlElectronics/finance/backend/internal/clientip"
	"github.com/AGlElectronics/finance/backend/internal/denlink"
	"github.com/AGlElectronics/finance/backend/internal/groupmatch"
)

// DefaultAudience is OIDC_AUDIENCE when unset: the audience Keycloak's
// audience mapper puts into Finance's access tokens.
const DefaultAudience = "finance-api"

type Config struct {
	// Auth is AUTH_MODE (default oidc) and APP_ENV (default production);
	// AUTH_MODE=dev is refused unless APP_ENV=development.
	Auth        authbase.Config
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

	DevDefaultUser string
	// DevDefaultGroups are the groups of a dev-mode caller that sends no
	// X-Dev-Groups header (DEV_DEFAULT_GROUPS).
	DevDefaultGroups []string

	OIDCIssuerURL string
	OIDCClientID  string
	// OIDCAudience must be in the access token's aud (OIDC_AUDIENCE,
	// default finance-api). ID tokens are refused.
	OIDCAudience string
	// Groups is how token groups are read and matched (OIDC_GROUPS_CLAIM,
	// OIDC_GROUP_MATCH, OIDC_GROUP_PREFIX).
	Groups groupmatch.Config
	// RoleGroups maps each Finance role (access.Role*) to the IdP groups that
	// grant it (FINANCE_ADMIN_GROUPS, FINANCE_APPROVER_GROUPS,
	// FINANCE_BOOKKEEPER_GROUPS, FINANCE_READONLY_GROUPS).
	RoleGroups map[string][]groupmatch.Grant

	// HRServiceToken lets HR read the role catalog (HR_SERVICE_TOKEN).
	HRServiceToken string
	// RateLimit (requests per second) and RateBurst bound each person's (or,
	// signed out, each address's) API calls (API_RATE_LIMIT, API_RATE_BURST).
	RateLimit float64
	RateBurst int

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

	// Den is the pairing configuration (DEN_MODE, DEN_INTERNAL_URL,
	// DEN_ALLOWED_HOSTS, DEN_LINK_ENCRYPTION_KEY[_PREVIOUS], APP_ENV).
	Den denlink.Config

	CORSAllowedOrigins []string
	// TrustedProxyCIDRs lists the proxies whose X-Forwarded-For entries are
	// believed when working out the client IP (TRUSTED_PROXY_CIDRS).
	TrustedProxyCIDRs []netip.Prefix

	ShutdownTimeout time.Duration
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		AppBaseURL:        getEnv("APP_BASE_URL", "http://localhost:8080"),
		HTTPAddr:          getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL:       getEnv("DATABASE_URL", ""),
		S3Endpoint:        getEnv("S3_ENDPOINT", ""),
		S3Bucket:          getEnv("S3_BUCKET", "finance-data-dev"),
		S3AccessKeyID:     getEnv("S3_ACCESS_KEY_ID", ""),
		S3SecretAccessKey: getEnv("S3_SECRET_ACCESS_KEY", ""),
		S3Region:          getEnv("S3_REGION", "us-east-1"),
		S3UsePathStyle:    getEnvBool("S3_USE_PATH_STYLE", true),
		DevDefaultUser:    getEnv("DEV_DEFAULT_USER", "dev-finance-admin"),
		DevDefaultGroups:  SplitCSV(getEnv("DEV_DEFAULT_GROUPS", access.RoleAdmin)),
		OIDCIssuerURL:     getEnv("OIDC_ISSUER_URL", ""),
		OIDCClientID:      getEnv("OIDC_CLIENT_ID", ""),
		OIDCAudience:      getEnv("OIDC_AUDIENCE", DefaultAudience),
		RoleGroups: map[string][]groupmatch.Grant{
			access.RoleAdmin:      Grants(getEnv("FINANCE_ADMIN_GROUPS", access.RoleAdmin)),
			access.RoleApprover:   Grants(getEnv("FINANCE_APPROVER_GROUPS", access.RoleApprover)),
			access.RoleBookkeeper: Grants(getEnv("FINANCE_BOOKKEEPER_GROUPS", access.RoleBookkeeper)),
			access.RoleReadOnly:   Grants(getEnv("FINANCE_READONLY_GROUPS", access.RoleReadOnly)),
		},
		HRServiceToken:     os.Getenv("HR_SERVICE_TOKEN"),
		Currency:           strings.ToUpper(getEnv("FINANCE_CURRENCY", "EUR")),
		InvoicePrefix:      getEnv("FINANCE_INVOICE_PREFIX", "FIN-"),
		CreditNotePrefix:   getEnv("FINANCE_CREDIT_NOTE_PREFIX", "FIN-CN-"),
		CORSAllowedOrigins: SplitCSV(getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3006")),
		ShutdownTimeout:    getEnvDuration("SHUTDOWN_TIMEOUT", 15*time.Second),
	}

	var errs []string
	var err error
	if cfg.Auth, err = authbase.FromEnv(os.Getenv); err != nil {
		errs = append(errs, err.Error())
	}
	if cfg.Groups, err = groupmatch.FromEnv(os.Getenv); err != nil {
		errs = append(errs, err.Error())
	}
	if cfg.Den, err = denlink.FromEnv("finance", "", os.Getenv); err != nil {
		errs = append(errs, err.Error())
	}
	if cfg.RateLimit, err = authbase.ParseRate(getEnv("API_RATE_LIMIT", "300/m")); err != nil {
		errs = append(errs, "API_RATE_LIMIT: "+err.Error())
	}
	if v, err := getEnvInt("API_RATE_BURST", 60); err != nil || v < 1 {
		errs = append(errs, "API_RATE_BURST must be a whole number of 1 or more")
	} else {
		cfg.RateBurst = int(v)
	}
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
	trusted, err := clientip.ParseCIDRs(getEnv("TRUSTED_PROXY_CIDRS", clientip.DefaultTrustedProxies))
	if err != nil {
		errs = append(errs, "TRUSTED_PROXY_CIDRS: "+err.Error())
	}
	cfg.TrustedProxyCIDRs = trusted

	if err := cfg.validate(); err != nil {
		errs = append(errs, strings.TrimPrefix(err.Error(), "invalid config: "))
	}
	if len(errs) > 0 {
		return nil, errors.New("invalid config: " + strings.Join(errs, "; "))
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
	if c.OIDCAudience == "" {
		errs = append(errs, "OIDC_AUDIENCE must not be empty")
	}
	for role, groups := range c.RoleGroups {
		if len(groups) == 0 {
			errs = append(errs, "the groups of role "+role+" must not be empty")
		}
	}
	if c.DatabaseURL == "" {
		errs = append(errs, "DATABASE_URL is required")
	}
	if !c.DevAuth() && c.OIDCIssuerURL == "" {
		errs = append(errs, "OIDC_ISSUER_URL is required when AUTH_MODE=oidc")
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

// DevAuth reports AUTH_MODE=dev with APP_ENV=development.
func (c *Config) DevAuth() bool { return c.Auth.DevAuth() }

// Grants parses a comma-separated group list into stored grants.
func Grants(csv string) []groupmatch.Grant {
	var out []groupmatch.Grant
	for _, g := range SplitCSV(csv) {
		out = append(out, groupmatch.NewGrant(g))
	}
	return out
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
func DefaultRoleGroups() map[string][]groupmatch.Grant {
	out := map[string][]groupmatch.Grant{}
	for _, r := range access.Roles {
		out[r] = []groupmatch.Grant{groupmatch.NewGrant(r)}
	}
	return out
}

// ForTest is a valid configuration for tests: dev sign-in in development,
// default roles and money settings, standalone Den.
func ForTest() *Config {
	return &Config{
		Auth:             authbase.Config{AuthMode: authbase.AuthDev, AppEnv: authbase.Development},
		DevDefaultUser:   "nobody",
		RoleGroups:       DefaultRoleGroups(),
		Den:              denlink.Config{Slug: "finance", AppEnv: authbase.Development, Mode: denlink.ModeStandalone},
		Currency:         "EUR",
		DefaultVATRateBp: 2100,
		InvoicePrefix:    "FIN-",
		CreditNotePrefix: "FIN-CN-",
		MaxBackdateDays:  366,
		RateLimit:        1000,
		RateBurst:        1000,
	}
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
		c.Auth.AppEnv, c.HTTPAddr, c.Auth.AuthMode, maskDSN(c.DatabaseURL), c.S3Bucket, c.CORSAllowedOrigins,
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
