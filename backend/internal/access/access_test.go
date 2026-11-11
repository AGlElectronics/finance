package access

import "testing"

func TestInAnyGroup(t *testing.T) {
	cases := []struct {
		have, want []string
		ok         bool
	}{
		{[]string{"/finance_admin"}, []string{"finance_admin"}, true},
		{[]string{"Finance_Admin"}, []string{"finance_admin"}, true},
		{[]string{"stash-volunteer", "dept-ops"}, []string{"finance_admin"}, false},
		{nil, []string{"finance_admin"}, false},
	}
	for _, c := range cases {
		if got := InAnyGroup(c.have, c.want); got != c.ok {
			t.Errorf("InAnyGroup(%v, %v) = %v, want %v", c.have, c.want, got, c.ok)
		}
	}
}

func TestResolveAndPermissions(t *testing.T) {
	groups := map[string][]string{
		RoleAdmin: {"fin-admins"}, RoleApprover: {"treasurer"}, RoleBookkeeper: {RoleBookkeeper}, RoleReadOnly: {"auditors"},
	}
	roles := Resolve([]string{"/treasurer", "finance_bookkeeper"}, groups)
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
