package access

import (
	"testing"

	"github.com/AGlElectronics/finance/backend/internal/groupmatch"
)

func grants(names ...string) []groupmatch.Grant {
	out := []groupmatch.Grant{}
	for _, n := range names {
		out = append(out, groupmatch.NewGrant(n))
	}
	return out
}

func TestInAnyGroup(t *testing.T) {
	full := groupmatch.Config{}
	cases := []struct {
		gm   groupmatch.Config
		have []string
		want []groupmatch.Grant
		ok   bool
	}{
		{full, []string{"/finance_admin"}, grants("finance_admin"), true},
		{full, []string{"Finance_Admin"}, grants("finance_admin"), true},
		{full, []string{"stash-volunteer", "dept-ops"}, grants("finance_admin"), false},
		{full, nil, grants("finance_admin"), false},
		{full, []string{"/teams/finance_admin"}, grants("finance_admin"), false},
		{groupmatch.Config{Mode: groupmatch.ModeLast}, []string{"/teams/finance_admin"}, grants("finance_admin"), true},
		{groupmatch.Config{Prefix: "/con2026"}, []string{"/con2026/finance_admin"}, grants("finance_admin"), true},
		{groupmatch.Config{Prefix: "/con2026"}, []string{"/con2025/finance_admin"}, grants("finance_admin"), false},
	}
	for _, c := range cases {
		if got := InAnyGroup(c.gm, c.have, c.want); got != c.ok {
			t.Errorf("InAnyGroup(%+v, %v, %v) = %v, want %v", c.gm, c.have, c.want, got, c.ok)
		}
	}
}

func TestResolveAndPermissions(t *testing.T) {
	groups := map[string][]groupmatch.Grant{
		RoleAdmin: grants("fin-admins"), RoleApprover: grants("Treasurer"), RoleBookkeeper: grants(RoleBookkeeper), RoleReadOnly: grants("auditors"),
	}
	roles := Resolve(groupmatch.Config{}, []string{"/treasurer", "finance_bookkeeper"}, groups)
	if len(roles) != 2 || roles[0] != RoleApprover || roles[1] != RoleBookkeeper {
		t.Fatalf("roles = %v", roles)
	}
	if !Has(roles, Write) || !Has(roles, Approve) || Has(roles, Admin) {
		t.Fatalf("permissions of %v = %v", roles, Permissions(roles))
	}
	if Has(nil, Read) {
		t.Fatal("no roles must grant nothing")
	}
	// Separation of duties: neither half of a four-eyes decision holds the other.
	if Has([]string{RoleApprover}, Write) || Has([]string{RoleBookkeeper}, Approve) {
		t.Fatal("approvers must not write and bookkeepers must not approve")
	}
}
