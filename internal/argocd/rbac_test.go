package argocd

import (
	"strings"
	"testing"
)

func TestUpdateRBACPolicy(t *testing.T) {
	initialPolicy := `p, role:org-admin, applications, *, */*, allow
g, alice, role:org-admin
`
	// 1. Add first user 'jenkins'
	p1 := UpdateRBACPolicy(initialPolicy, "jenkins", []string{"proj:my-project:admin", "proj:public:read"})

	if !strings.Contains(p1, ManagedBlockStart) || !strings.Contains(p1, ManagedBlockEnd) {
		t.Fatalf("Expected managed block markers in policy:\n%s", p1)
	}
	if !strings.Contains(p1, "p, role:org-admin, applications, *, */*, allow") {
		t.Fatalf("Expected existing policy to be preserved:\n%s", p1)
	}
	if !strings.Contains(p1, "g, jenkins, proj:my-project:admin") || !strings.Contains(p1, "g, jenkins, proj:public:read") {
		t.Fatalf("Expected jenkins roles in policy:\n%s", p1)
	}

	// 2. Add second user 'gitlab'
	p2 := UpdateRBACPolicy(p1, "gitlab", []string{"role:readonly"})
	if !strings.Contains(p2, "g, jenkins, proj:my-project:admin") {
		t.Fatalf("Expected jenkins to remain:\n%s", p2)
	}
	if !strings.Contains(p2, "g, gitlab, role:readonly") {
		t.Fatalf("Expected gitlab role to be added:\n%s", p2)
	}

	// 3. Update 'jenkins' roles
	p3 := UpdateRBACPolicy(p2, "jenkins", []string{"role:admin"})
	if strings.Contains(p3, "proj:my-project:admin") {
		t.Fatalf("Old jenkins roles should have been removed:\n%s", p3)
	}
	if !strings.Contains(p3, "g, jenkins, role:admin") {
		t.Fatalf("New jenkins role should be present:\n%s", p3)
	}
	if !strings.Contains(p3, "g, gitlab, role:readonly") {
		t.Fatalf("Gitlab role should still be present:\n%s", p3)
	}

	// 4. Remove 'jenkins'
	p4 := RemoveRBACUser(p3, "jenkins")
	if strings.Contains(p4, "jenkins") {
		t.Fatalf("Expected jenkins to be removed from policy:\n%s", p4)
	}
	if !strings.Contains(p4, "g, gitlab, role:readonly") {
		t.Fatalf("Gitlab role should still be present:\n%s", p4)
	}
	if !strings.Contains(p4, "p, role:org-admin, applications, *, */*, allow") {
		t.Fatalf("Original policy outside managed block must be preserved:\n%s", p4)
	}
}

func TestParseRoles(t *testing.T) {
	input := "proj:alpha:admin, role:readonly,  proj:beta:read \n role:admin,proj:alpha:admin"
	roles := ParseRoles(input)

	expected := []string{
		"proj:alpha:admin",
		"role:readonly",
		"proj:beta:read",
		"role:admin",
	}

	if len(roles) != len(expected) {
		t.Fatalf("Expected %d roles, got %d: %v", len(expected), len(roles), roles)
	}

	for i, exp := range expected {
		if roles[i] != exp {
			t.Errorf("At index %d expected %s, got %s", i, exp, roles[i])
		}
	}
}
