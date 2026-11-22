package argocd

import (
	"fmt"
	"strings"
)

const (
	ManagedBlockStart = "# --- ARGOLUM MANAGED USERS BEGIN ---"
	ManagedBlockEnd   = "# --- ARGOLUM MANAGED USERS END ---"
)

// UpdateRBACPolicy updates or adds roles for a specific username inside the dedicated argolum section of policy.csv.
// If roles is empty, all roles for the user are removed from the managed section.
// Existing policies outside the managed section are preserved verbatim.
func UpdateRBACPolicy(existingPolicy, username string, roles []string) string {
	username = strings.TrimSpace(username)
	if username == "" {
		return existingPolicy
	}

	lines := strings.Split(existingPolicy, "\n")

	var beforeBlock []string
	var managedLines []string
	var afterBlock []string

	inManagedBlock := false
	blockFound := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == ManagedBlockStart {
			inManagedBlock = true
			blockFound = true
			continue
		}
		if trimmed == ManagedBlockEnd && inManagedBlock {
			inManagedBlock = false
			continue
		}

		if inManagedBlock {
			managedLines = append(managedLines, line)
		} else if !blockFound {
			beforeBlock = append(beforeBlock, line)
		} else {
			afterBlock = append(afterBlock, line)
		}
	}

	// Filter out existing rules for this user from the managed block
	userPrefix := fmt.Sprintf("g, %s,", username)
	var filteredManaged []string
	for _, mLine := range managedLines {
		t := strings.TrimSpace(mLine)
		if strings.HasPrefix(t, userPrefix) || strings.HasPrefix(t, fmt.Sprintf("g,%s,", username)) {
			continue
		}
		if t != "" {
			filteredManaged = append(filteredManaged, mLine)
		}
	}

	// Add new roles for this user
	for _, role := range roles {
		role = strings.TrimSpace(role)
		if role != "" {
			filteredManaged = append(filteredManaged, fmt.Sprintf("g, %s, %s", username, role))
		}
	}

	var result []string

	// Clean trailing empty lines from beforeBlock
	for len(beforeBlock) > 0 && strings.TrimSpace(beforeBlock[len(beforeBlock)-1]) == "" {
		beforeBlock = beforeBlock[:len(beforeBlock)-1]
	}

	if len(beforeBlock) > 0 {
		result = append(result, beforeBlock...)
	}

	// Only emit managed block if there are managed entries or if block already existed
	if len(filteredManaged) > 0 || blockFound {
		if len(result) > 0 {
			result = append(result, "") // blank separator
		}
		result = append(result, ManagedBlockStart)
		result = append(result, filteredManaged...)
		result = append(result, ManagedBlockEnd)
	}

	// Clean leading empty lines from afterBlock
	for len(afterBlock) > 0 && strings.TrimSpace(afterBlock[0]) == "" {
		afterBlock = afterBlock[1:]
	}

	if len(afterBlock) > 0 {
		if len(result) > 0 {
			result = append(result, "") // blank separator
		}
		result = append(result, afterBlock...)
	}

	joined := strings.Join(result, "\n")
	if !strings.HasSuffix(joined, "\n") && joined != "" {
		joined += "\n"
	}
	return joined
}

// RemoveRBACUser removes all managed roles for a user from the policy.
func RemoveRBACUser(existingPolicy, username string) string {
	return UpdateRBACPolicy(existingPolicy, username, nil)
}

// ParseRoles parses a comma-separated or newline-separated roles string into a slice of cleaned role names.
func ParseRoles(rolesStr string) []string {
	if strings.TrimSpace(rolesStr) == "" {
		return nil
	}
	// Split on both commas and newlines
	normalized := strings.ReplaceAll(rolesStr, "\n", ",")
	rawRoles := strings.Split(normalized, ",")
	var roles []string
	seen := make(map[string]bool)
	for _, r := range rawRoles {
		trimmed := strings.TrimSpace(r)
		if trimmed != "" && !seen[trimmed] {
			roles = append(roles, trimmed)
			seen[trimmed] = true
		}
	}
	return roles
}
