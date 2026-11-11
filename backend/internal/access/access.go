// Package access defines Finance's roles and the permissions each route
// checks. A person gets a role by being in one of the role's IdP groups
// (assigned in HR or directly in the IdP); the role catalog publishes them.
package access

import "strings"

// Permission is checked per route.
type Permission string

const (
	// Read: dashboard, customers, invoices, credit notes, expenses, ledger,
	// approvals, periods, settings, PDFs.
	Read Permission = "finance.read"
	// Write: customers, drafts, issuing, and requesting voids, payments,
	// credit notes, refunds and expenses.
	Write Permission = "finance.write"
	// Approve: decide four-eyes requests and close months.
	Approve Permission = "finance.approve"
	// Audit: read the audit log.
	Audit Permission = "finance.audit"
	// Admin: reopen months, change settings, pair with Den.
	Admin Permission = "finance.admin"
)

// Role keys as published in the role catalog.
const (
	RoleAdmin      = "finance_admin"
	RoleApprover   = "finance_approver"
	RoleBookkeeper = "finance_bookkeeper"
	RoleReadOnly   = "finance_read-only"
)

// Roles lists every role, most powerful first.
var Roles = []string{RoleAdmin, RoleApprover, RoleBookkeeper, RoleReadOnly}

// Grants is what each role may do. Approvers do not write and bookkeepers
// do not approve, so the two halves of a four-eyes decision stay apart.
var Grants = map[string][]Permission{
	RoleAdmin:      {Read, Write, Approve, Audit, Admin},
	RoleApprover:   {Read, Approve, Audit},
	RoleBookkeeper: {Read, Write},
	RoleReadOnly:   {Read, Audit},
}

// Permissions returns the union of the permissions of roles.
func Permissions(roles []string) []Permission {
	seen := map[Permission]bool{}
	out := []Permission{}
	for _, r := range roles {
		for _, p := range Grants[r] {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// Has reports whether roles grant p.
func Has(roles []string, p Permission) bool {
	for _, r := range roles {
		for _, g := range Grants[r] {
			if g == p {
				return true
			}
		}
	}
	return false
}

// Resolve returns the roles whose groups (roleGroups[role]) contain one of
// the caller's groups.
func Resolve(groups []string, roleGroups map[string][]string) []string {
	out := []string{}
	for _, role := range Roles {
		if InAnyGroup(groups, roleGroups[role]) {
			out = append(out, role)
		}
	}
	return out
}

// InAnyGroup matches group names ignoring case and Keycloak's leading "/".
// This is Finance's own copy of the matching rule; the suite-wide shared
// rule (full group paths, configurable claim) replaces it when it lands.
func InAnyGroup(have, want []string) bool {
	for _, h := range have {
		h = strings.TrimPrefix(h, "/")
		for _, w := range want {
			if strings.EqualFold(h, strings.TrimPrefix(w, "/")) {
				return true
			}
		}
	}
	return false
}
