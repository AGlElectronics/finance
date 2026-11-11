// Package catalog serves Finance's role catalog.
//
// Contract: each Con Services module serves a public `GET /api/role-catalog`
// returning
//
//	{"module","display_name","description","roles":[{"key","label",
//	  "description","group","assignable","scope","permissions","note"}]}
//
// HR aggregates these catalogs (ROLE_CATALOG_SOURCES) into its access picker
// and assigns a role by adding the person to the role's IdP `group`.
package catalog

import "github.com/AGlElectronics/finance/backend/internal/access"

// Role is one entry in the module's catalog.
type Role struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Group       string   `json:"group"`
	Assignable  bool     `json:"assignable"`
	Scope       string   `json:"scope"`
	Permissions []string `json:"permissions"`
	Note        string   `json:"note,omitempty"`
	// Default roles are ticked for every new onboarding; HR can untick them.
	Default bool `json:"default,omitempty"`
}

// Module is the catalog document.
type Module struct {
	Module      string `json:"module"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Roles       []Role `json:"roles"`
	Source      string `json:"source"`
	Status      string `json:"status"` // ok | error
	Error       string `json:"error,omitempty"`
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

// Self is Finance's own role catalog. roleGroups is config.RoleGroups; the
// first group of each role is the one HR assigns.
func Self(roleGroups map[string][]string) Module {
	roles := make([]Role, 0, len(access.Roles))
	for _, key := range access.Roles {
		group := key
		if gs := roleGroups[key]; len(gs) > 0 {
			group = gs[0]
		}
		perms := []string{}
		for _, p := range access.Grants[key] {
			perms = append(perms, string(p))
		}
		roles = append(roles, Role{
			Key:         key,
			Label:       roleText[key][0],
			Description: roleText[key][1],
			Group:       group,
			Assignable:  true,
			Scope:       "global",
			Permissions: perms,
		})
	}
	return Module{
		Module:      "finance",
		DisplayName: "Finance",
		Description: "Invoices and money tracking.",
		Source:      "self",
		Status:      "ok",
		Roles:       roles,
	}
}
