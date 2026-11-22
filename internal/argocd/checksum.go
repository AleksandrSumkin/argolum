package argocd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// UserSpec contains normalized fields parsed from a user secret.
type UserSpec struct {
	Username string
	Password string
	Enabled  bool
	APIKey   bool
	Login    bool
	Roles    string
}

// CalculateChecksum computes a canonical SHA-256 hash for user attributes.
func CalculateChecksum(spec UserSpec) string {
	raw := fmt.Sprintf("%s|%s|%t|%t|%t|%s",
		spec.Username,
		spec.Password,
		spec.Enabled,
		spec.APIKey,
		spec.Login,
		spec.Roles,
	)
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}
