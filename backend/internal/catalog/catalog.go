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

// Self is Finance's own role catalog.
func Self() Module {
	return Module{
		Module:      "finance",
		DisplayName: "Finance",
		Description: "Invoices and money tracking.",
		Source:      "self",
		Status:      "ok",
		Roles: []Role{
			{
				Key:         "finance-admin",
				Label:       "Finance admin",
				Description: "Full access: customers, invoices and payments.",
				Group:       "finance-admin",
				Assignable:  true,
				Scope:       "global",
				Permissions: []string{"finance.*"},
			},
		},
	}
}
