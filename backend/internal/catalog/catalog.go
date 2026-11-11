// Package catalog serves Finance's role catalog (GET /api/role-catalog).
//
// Contract (con-services docs: concepts/role-catalog): the catalog lists
// every role with the IdP group that grants it, whether it is privileged
// and what it permits. HR reads it with HR_SERVICE_TOKEN; a person signed in
// to the staff realm may read it too. HR assigns a role by adding the person
// to the role's group.
package catalog

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/AGlElectronics/finance/backend/internal/access"
	"github.com/AGlElectronics/finance/backend/internal/groupmatch"
)

// Role is one entry in the module's catalog.
type Role struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	// Group is the stored (normalised) group; GroupDisplay as configured.
	Group        string   `json:"group"`
	GroupDisplay string   `json:"group_display"`
	Assignable   bool     `json:"assignable"`
	Default      bool     `json:"default"`
	Scope        string   `json:"scope"`
	Privileged   bool     `json:"privileged"`
	Dynamic      bool     `json:"dynamic"`
	Permissions  []string `json:"permissions"`
}

// Module is the catalog document.
type Module struct {
	Module      string `json:"module"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	// Revision changes whenever the list changes (also the ETag).
	Revision string `json:"revision"`
	Roles    []Role `json:"roles"`
}

var roleText = map[string][2]string{
	access.RoleAdmin: {"Finance admin",
		"Everything a bookkeeper and an approver can do, plus reopening closed months, Finance settings and the Den connection."},
	access.RoleApprover: {"Finance approver",
		"Approves or rejects voids, payments, expenses, credit notes and refunds requested by someone else, and closes months. Cannot record money itself."},
	access.RoleBookkeeper: {"Finance bookkeeper",
		"Keeps customers, invoices and expenses, and requests payments, voids, credit notes and refunds."},
	access.RoleReadOnly: {"Finance read-only",
		"Sees invoices, expenses, the ledger and the audit log; changes nothing. For accountants and auditors."},
}

// privileged roles change settings or release money (four-eyes approval).
var privileged = map[string]bool{access.RoleAdmin: true, access.RoleApprover: true}

// Self is Finance's own role catalog. roleGroups is config.RoleGroups; the
// first group of each role is the one HR assigns.
func Self(roleGroups map[string][]groupmatch.Grant) Module {
	roles := make([]Role, 0, len(access.Roles))
	for _, key := range access.Roles {
		grant := groupmatch.NewGrant(key)
		if gs := roleGroups[key]; len(gs) > 0 {
			grant = gs[0]
		}
		perms := []string{}
		for _, p := range access.Grants[key] {
			perms = append(perms, string(p))
		}
		roles = append(roles, Role{
			Key:          key,
			Label:        roleText[key][0],
			Description:  roleText[key][1],
			Group:        grant.Stored,
			GroupDisplay: grant.Display,
			Assignable:   true,
			Scope:        "organization",
			Privileged:   privileged[key],
			Permissions:  perms,
		})
	}
	m := Module{
		Module:      "finance",
		DisplayName: "Finance",
		Description: "Invoices and money tracking.",
		Roles:       roles,
	}
	b, _ := json.Marshal(roles)
	sum := sha256.Sum256(b)
	m.Revision = hex.EncodeToString(sum[:8])
	return m
}

// Handler serves the catalog. allowed decides whether the caller may read
// it (HR's service token or a staff sign-in); it answers 401 otherwise.
func Handler(m Module, allowed func(r *http.Request) bool) http.HandlerFunc {
	etag := `"` + m.Revision + `"`
	body, _ := json.Marshal(m)
	return func(w http.ResponseWriter, r *http.Request) {
		if !allowed(r) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"authentication required"}}` + "\n"))
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "private, no-cache")
		if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(append(body, '\n'))
	}
}

// ServiceToken reports whether r carries the HR service token (constant
// time). An empty configured token never matches.
func ServiceToken(r *http.Request, want string) bool {
	if want == "" {
		return false
	}
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(h) <= len(p) || !strings.EqualFold(h[:len(p)], p) {
		return false
	}
	got := strings.TrimSpace(h[len(p):])
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
