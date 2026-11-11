// Package access defines Finance's roles and the permissions each route
// checks. A person gets a role by being in one of the role's IdP groups
// (assigned in HR or directly in the IdP); the role catalog publishes them.
package access

import "github.com/AGlElectronics/finance/backend/internal/groupmatch"

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

// Resolve returns the roles whose groups (roleGroups[role]) match one of the
// caller's groups under the shared matching rule (gm).
func Resolve(gm groupmatch.Config, groups []string, roleGroups map[string][]groupmatch.Grant) []string {
	out := []string{}
	for _, role := range Roles {
		if InAnyGroup(gm, groups, roleGroups[role]) {
			out = append(out, role)
		}
	}
	return out
}

// InAnyGroup reports whether one of have matches one of the grants.
func InAnyGroup(gm groupmatch.Config, have []string, grants []groupmatch.Grant) bool {
	want := make([]string, 0, len(grants))
	for _, g := range grants {
		want = append(want, g.Stored)
	}
	return gm.MatchAny(have, want)
}
