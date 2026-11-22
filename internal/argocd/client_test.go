package argocd

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/aleksandrsumkin/argolum/internal/config"
)

func setupTestClient(t *testing.T) (*Client, *config.Config, *runtime.Scheme) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "argocd",
			Name:      "argocd-cm",
		},
		Data: map[string]string{
			"admin.enabled": "true",
		},
	}
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "argocd",
			Name:      "argocd-secret",
		},
		Data: map[string][]byte{
			"admin.password": []byte("$2a$10$dummy"),
		},
	}
	rbacCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "argocd",
			Name:      "argocd-rbac-cm",
		},
		Data: map[string]string{
			"policy.csv": "p, role:admin, *, */*, allow\n",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cm, sec, rbacCM).
		Build()

	cfg := &config.Config{
		WatchNamespace:   "argocd",
		ArgocdCMName:     "argocd-cm",
		ArgocdSecretName: "argocd-secret",
		ArgocdRBACCMName: "argocd-rbac-cm",
		BcryptCost:       4, // faster for testing
		IgnoreUsers:      []string{"admin"},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := NewClient(fakeClient, cfg, logger)

	return client, cfg, scheme
}

func TestSyncUser_Success(t *testing.T) {
	c, _, _ := setupTestClient(t)
	ctx := context.Background()

	spec := UserSpec{
		Username: "jenkins",
		Password: "plainPassword123!",
		Enabled:  true,
		APIKey:   true,
		Login:    true,
		Roles:    "proj:app:admin, role:readonly",
	}

	err := c.SyncUser(ctx, "argocd", spec)
	if err != nil {
		t.Fatalf("SyncUser returned error: %v", err)
	}

	// Verify argocd-cm
	var cm corev1.ConfigMap
	if err := c.k8sClient.Get(ctx, types.NamespacedName{Namespace: "argocd", Name: "argocd-cm"}, &cm); err != nil {
		t.Fatalf("Failed to get cm: %v", err)
	}
	if cm.Data["accounts.jenkins"] != "apiKey, login" {
		t.Errorf("Unexpected accounts.jenkins: %s", cm.Data["accounts.jenkins"])
	}
	if cm.Data["accounts.jenkins.enabled"] != "true" {
		t.Errorf("Unexpected accounts.jenkins.enabled: %s", cm.Data["accounts.jenkins.enabled"])
	}

	// Verify argocd-secret
	var sec corev1.Secret
	if err := c.k8sClient.Get(ctx, types.NamespacedName{Namespace: "argocd", Name: "argocd-secret"}, &sec); err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}
	passHash := string(sec.Data["accounts.jenkins.password"])
	if !strings.HasPrefix(passHash, "$2a$") {
		t.Errorf("Password was not properly hashed with bcrypt: %s", passHash)
	}
	if len(sec.Data["accounts.jenkins.passwordMtime"]) == 0 {
		t.Errorf("passwordMtime should be populated")
	}

	// Verify argocd-rbac-cm
	var rbacCM corev1.ConfigMap
	if err := c.k8sClient.Get(ctx, types.NamespacedName{Namespace: "argocd", Name: "argocd-rbac-cm"}, &rbacCM); err != nil {
		t.Fatalf("Failed to get rbac cm: %v", err)
	}
	policy := rbacCM.Data["policy.csv"]
	if !strings.Contains(policy, "g, jenkins, proj:app:admin") || !strings.Contains(policy, "g, jenkins, role:readonly") {
		t.Errorf("RBAC policy missing jenkins roles:\n%s", policy)
	}
}

func TestSyncUser_AdminProtected(t *testing.T) {
	c, _, _ := setupTestClient(t)
	ctx := context.Background()

	adminSpec := UserSpec{
		Username: "admin",
		Password: "hackedPassword",
		Enabled:  true,
		APIKey:   true,
		Login:    true,
	}

	// Should be safely ignored
	err := c.SyncUser(ctx, "argocd", adminSpec)
	if err != nil {
		t.Fatalf("SyncUser should not return error on ignored user: %v", err)
	}

	// Check that admin password in secret was not overwritten
	var sec corev1.Secret
	if err := c.k8sClient.Get(ctx, types.NamespacedName{Namespace: "argocd", Name: "argocd-secret"}, &sec); err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}
	if string(sec.Data["admin.password"]) != "$2a$10$dummy" {
		t.Errorf("Admin password was altered: %s", string(sec.Data["admin.password"]))
	}
}

func TestDeleteUser(t *testing.T) {
	c, _, _ := setupTestClient(t)
	ctx := context.Background()

	spec := UserSpec{
		Username: "deployer",
		Password: "pass",
		Enabled:  true,
		APIKey:   false,
		Login:    true,
		Roles:    "role:readonly",
	}

	_ = c.SyncUser(ctx, "argocd", spec)

	// Now delete user
	err := c.DeleteUser(ctx, "argocd", "deployer")
	if err != nil {
		t.Fatalf("DeleteUser returned error: %v", err)
	}

	var cm corev1.ConfigMap
	_ = c.k8sClient.Get(ctx, types.NamespacedName{Namespace: "argocd", Name: "argocd-cm"}, &cm)
	if _, exists := cm.Data["accounts.deployer"]; exists {
		t.Errorf("accounts.deployer should have been deleted from cm")
	}

	var sec corev1.Secret
	_ = c.k8sClient.Get(ctx, types.NamespacedName{Namespace: "argocd", Name: "argocd-secret"}, &sec)
	if _, exists := sec.Data["accounts.deployer.password"]; exists {
		t.Errorf("accounts.deployer.password should have been deleted from secret")
	}

	var rbacCM corev1.ConfigMap
	_ = c.k8sClient.Get(ctx, types.NamespacedName{Namespace: "argocd", Name: "argocd-rbac-cm"}, &rbacCM)
	if strings.Contains(rbacCM.Data["policy.csv"], "deployer") {
		t.Errorf("deployer should have been deleted from rbac policy")
	}
}
