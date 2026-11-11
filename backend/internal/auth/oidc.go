package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/AGlElectronics/finance/backend/internal/config"
	"github.com/AGlElectronics/finance/backend/internal/groupmatch"
)

// OIDCVerifier verifies Bearer access tokens from the shared Keycloak realm:
// signature, issuer, expiry and that aud contains OIDC_AUDIENCE (Finance's
// API, finance-api by default). ID tokens are refused.
type OIDCVerifier struct {
	verifier *oidc.IDTokenVerifier
	issuer   string
	groups   groupmatch.Config
}

// Claims is what Finance reads from a verified token.
type Claims struct {
	Issuer            string
	Subject           string
	Email             string
	PreferredUsername string
	Name              string
	// Groups come only from the claim named by OIDC_GROUPS_CLAIM.
	Groups []string
}

// NewOIDCVerifier discovers the issuer and builds a verifier for Finance's
// API audience.
func NewOIDCVerifier(ctx context.Context, cfg *config.Config) (*OIDCVerifier, error) {
	if cfg.OIDCIssuerURL == "" {
		return nil, errors.New("OIDC verifier requires OIDC_ISSUER_URL")
	}
	provider, err := oidc.NewProvider(ctx, cfg.OIDCIssuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	return NewOIDCVerifierFrom(provider.Verifier(&oidc.Config{ClientID: cfg.OIDCAudience}), cfg.OIDCIssuerURL, cfg.Groups), nil
}

// NewOIDCVerifierFrom wraps an existing go-oidc verifier (tests).
func NewOIDCVerifierFrom(v *oidc.IDTokenVerifier, issuer string, groups groupmatch.Config) *OIDCVerifier {
	return &OIDCVerifier{verifier: v, issuer: issuer, groups: groups}
}

// Verify validates a bearer access token and returns its claims.
func (v *OIDCVerifier) Verify(ctx context.Context, bearer string) (*Claims, error) {
	bearer = strings.TrimSpace(bearer)
	if bearer == "" {
		return nil, errors.New("missing bearer token")
	}
	tok, err := v.verifier.Verify(ctx, bearer)
	if err != nil {
		return nil, fmt.Errorf("verify token: %w", err)
	}
	var raw map[string]any
	if err := tok.Claims(&raw); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}
	// Keycloak marks ID tokens typ "ID" and access tokens "Bearer".
	if typ, _ := raw["typ"].(string); strings.EqualFold(typ, "ID") {
		return nil, errors.New("an ID token was sent; send the access token")
	}
	str := func(k string) string { s, _ := raw[k].(string); return s }
	c := &Claims{
		Issuer:            v.issuer,
		Subject:           tok.Subject,
		Email:             str("email"),
		PreferredUsername: str("preferred_username"),
		Name:              str("name"),
		Groups:            v.groups.Groups(raw),
	}
	if c.Subject == "" {
		return nil, errors.New("token missing sub claim")
	}
	return c, nil
}

// DisplayName picks a human label from claims.
func DisplayName(c *Claims) string {
	for _, candidate := range []string{c.PreferredUsername, c.Email, c.Name} {
		if s := strings.TrimSpace(candidate); s != "" {
			return s
		}
	}
	return c.Subject
}
