package argocd

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/aleksandrsumkin/argolum/internal/config"
)

// Client manages synchronization with ArgoCD ConfigMaps and Secret.
type Client struct {
	k8sClient client.Client
	cfg       *config.Config
	logger    *slog.Logger

	mu           sync.RWMutex
	cmName       string
	secretName   string
	rbacCMName   string
	detectedOnce bool
}

// NewClient creates a new ArgoCD client.
func NewClient(k8sClient client.Client, cfg *config.Config, logger *slog.Logger) *Client {
	return &Client{
		k8sClient:  k8sClient,
		cfg:        cfg,
		logger:     logger,
		cmName:     cfg.ArgocdCMName,
		secretName: cfg.ArgocdSecretName,
		rbacCMName: cfg.ArgocdRBACCMName,
	}
}

// DiscoverResourceNames attempts to resolve the exact names of ArgoCD resources.
func (c *Client) DiscoverResourceNames(ctx context.Context, namespace string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// CM name resolution
	if c.cmName == "" {
		c.cmName = c.resolveConfigMapName(ctx, namespace, []string{"argocd-cm"}, "argocd-cm")
	}

	// Secret name resolution
	if c.secretName == "" {
		c.secretName = c.resolveSecretName(ctx, namespace, []string{"argocd-secret"}, "argocd-secret")
	}

	// RBAC CM name resolution
	if c.rbacCMName == "" {
		c.rbacCMName = c.resolveConfigMapName(ctx, namespace, []string{"argocd-rbac-cm", "argocd-cm-rbac"}, "argocd-rbac-cm")
	}

	c.detectedOnce = true
	c.logger.Info("ArgoCD resources resolved",
		slog.String("namespace", namespace),
		slog.String("argocd-cm", c.cmName),
		slog.String("argocd-secret", c.secretName),
		slog.String("argocd-rbac-cm", c.rbacCMName),
	)
	return nil
}

func (c *Client) resolveConfigMapName(ctx context.Context, namespace string, candidates []string, fallback string) string {
	for _, name := range candidates {
		var cm corev1.ConfigMap
		if err := c.k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &cm); err == nil {
			return name
		}
	}
	return fallback
}

func (c *Client) resolveSecretName(ctx context.Context, namespace string, candidates []string, fallback string) string {
	for _, name := range candidates {
		var sec corev1.Secret
		if err := c.k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &sec); err == nil {
			return name
		}
	}
	return fallback
}

// IsIgnoredUser checks whether a username is protected from management.
func (c *Client) IsIgnoredUser(username string) bool {
	if strings.EqualFold(username, "admin") || strings.EqualFold(username, "administrator") {
		return true
	}
	for _, u := range c.cfg.IgnoreUsers {
		if strings.EqualFold(username, u) {
			return true
		}
	}
	return false
}

// SyncUser reconciles a user into argocd-cm, argocd-secret, and argocd-rbac-cm.
func (c *Client) SyncUser(ctx context.Context, namespace string, spec UserSpec) error {
	if c.IsIgnoredUser(spec.Username) {
		c.logger.Warn("Refusing to manage protected user account",
			slog.String("username", spec.Username),
			slog.String("namespace", namespace),
		)
		return nil
	}

	if !c.detectedOnce {
		_ = c.DiscoverResourceNames(ctx, namespace)
	}

	c.mu.RLock()
	cmName := c.cmName
	secretName := c.secretName
	rbacName := c.rbacCMName
	c.mu.RUnlock()

	// 1. Update argocd-cm
	if err := c.syncConfigMap(ctx, namespace, cmName, spec); err != nil {
		return fmt.Errorf("failed to sync argocd-cm: %w", err)
	}

	// 2. Update argocd-secret
	if err := c.syncSecret(ctx, namespace, secretName, spec); err != nil {
		return fmt.Errorf("failed to sync argocd-secret: %w", err)
	}

	// 3. Update argocd-rbac-cm
	roles := ParseRoles(spec.Roles)
	if err := c.syncRBAC(ctx, namespace, rbacName, spec.Username, roles); err != nil {
		return fmt.Errorf("failed to sync argocd-rbac-cm: %w", err)
	}

	c.logger.Info("Successfully synchronized user to ArgoCD",
		slog.String("username", spec.Username),
		slog.Bool("enabled", spec.Enabled),
		slog.Int("rolesCount", len(roles)),
	)
	return nil
}

func (c *Client) syncConfigMap(ctx context.Context, namespace, name string, spec UserSpec) error {
	var cm corev1.ConfigMap
	err := c.k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &cm)
	if err != nil {
		return err
	}
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}

	// Capabilities string
	var caps []string
	if spec.APIKey {
		caps = append(caps, "apiKey")
	}
	if spec.Login {
		caps = append(caps, "login")
	}
	capVal := strings.Join(caps, ", ")

	accountKey := fmt.Sprintf("accounts.%s", spec.Username)
	enabledKey := fmt.Sprintf("accounts.%s.enabled", spec.Username)

	enabledVal := "false"
	if spec.Enabled {
		enabledVal = "true"
	}

	changed := false
	if cm.Data[accountKey] != capVal {
		cm.Data[accountKey] = capVal
		changed = true
	}
	if cm.Data[enabledKey] != enabledVal {
		cm.Data[enabledKey] = enabledVal
		changed = true
	}

	if changed {
		return c.k8sClient.Update(ctx, &cm)
	}
	return nil
}

func (c *Client) syncSecret(ctx context.Context, namespace, name string, spec UserSpec) error {
	var sec corev1.Secret
	err := c.k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &sec)
	if err != nil {
		return err
	}
	if sec.Data == nil {
		sec.Data = make(map[string][]byte)
	}

	hashedPassword, err := HashPassword(spec.Password, c.cfg.BcryptCost)
	if err != nil {
		return fmt.Errorf("failed to bcrypt hash password: %w", err)
	}

	passKey := fmt.Sprintf("accounts.%s.password", spec.Username)
	mtimeKey := fmt.Sprintf("accounts.%s.passwordMtime", spec.Username)

	sec.Data[passKey] = []byte(hashedPassword)
	sec.Data[mtimeKey] = []byte(time.Now().UTC().Format(time.RFC3339))

	return c.k8sClient.Update(ctx, &sec)
}

func (c *Client) syncRBAC(ctx context.Context, namespace, name, username string, roles []string) error {
	var cm corev1.ConfigMap
	err := c.k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &cm)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// If RBAC ConfigMap doesn't exist yet, we can create it
			cm = corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace,
					Name:      name,
				},
				Data: make(map[string]string),
			}
			newPolicy := UpdateRBACPolicy("", username, roles)
			cm.Data["policy.csv"] = newPolicy
			return c.k8sClient.Create(ctx, &cm)
		}
		return err
	}

	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}

	currentPolicy := cm.Data["policy.csv"]
	newPolicy := UpdateRBACPolicy(currentPolicy, username, roles)

	if currentPolicy != newPolicy {
		cm.Data["policy.csv"] = newPolicy
		return c.k8sClient.Update(ctx, &cm)
	}
	return nil
}

// DeleteUser removes a user and their associated configuration from ArgoCD resources.
func (c *Client) DeleteUser(ctx context.Context, namespace, username string) error {
	if c.IsIgnoredUser(username) {
		c.logger.Warn("Refusing to delete protected user account",
			slog.String("username", username),
			slog.String("namespace", namespace),
		)
		return nil
	}

	if !c.detectedOnce {
		_ = c.DiscoverResourceNames(ctx, namespace)
	}

	c.mu.RLock()
	cmName := c.cmName
	secretName := c.secretName
	rbacName := c.rbacCMName
	c.mu.RUnlock()

	// 1. Delete from argocd-cm
	var cm corev1.ConfigMap
	if err := c.k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cmName}, &cm); err == nil && cm.Data != nil {
		accountKey := fmt.Sprintf("accounts.%s", username)
		enabledKey := fmt.Sprintf("accounts.%s.enabled", username)
		changed := false
		if _, exists := cm.Data[accountKey]; exists {
			delete(cm.Data, accountKey)
			changed = true
		}
		if _, exists := cm.Data[enabledKey]; exists {
			delete(cm.Data, enabledKey)
			changed = true
		}
		if changed {
			if err := c.k8sClient.Update(ctx, &cm); err != nil {
				return fmt.Errorf("failed to update argocd-cm on user delete: %w", err)
			}
		}
	}

	// 2. Delete from argocd-secret
	var sec corev1.Secret
	if err := c.k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: secretName}, &sec); err == nil && sec.Data != nil {
		passKey := fmt.Sprintf("accounts.%s.password", username)
		mtimeKey := fmt.Sprintf("accounts.%s.passwordMtime", username)
		tokenKey := fmt.Sprintf("accounts.%s.tokens", username)
		changed := false
		if _, exists := sec.Data[passKey]; exists {
			delete(sec.Data, passKey)
			changed = true
		}
		if _, exists := sec.Data[mtimeKey]; exists {
			delete(sec.Data, mtimeKey)
			changed = true
		}
		if _, exists := sec.Data[tokenKey]; exists {
			delete(sec.Data, tokenKey)
			changed = true
		}
		if changed {
			if err := c.k8sClient.Update(ctx, &sec); err != nil {
				return fmt.Errorf("failed to update argocd-secret on user delete: %w", err)
			}
		}
	}

	// 3. Delete from argocd-rbac-cm
	var rbacCM corev1.ConfigMap
	if err := c.k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: rbacName}, &rbacCM); err == nil && rbacCM.Data != nil {
		currentPolicy := rbacCM.Data["policy.csv"]
		newPolicy := RemoveRBACUser(currentPolicy, username)
		if currentPolicy != newPolicy {
			rbacCM.Data["policy.csv"] = newPolicy
			if err := c.k8sClient.Update(ctx, &rbacCM); err != nil {
				return fmt.Errorf("failed to update argocd-rbac-cm on user delete: %w", err)
			}
		}
	}

	c.logger.Info("Successfully removed user from ArgoCD",
		slog.String("username", username),
		slog.String("namespace", namespace),
	)
	return nil
}

// PruneUsers removes all local users in argocd-cm that do not exist in activeUsers and are not ignored.
func (c *Client) PruneUsers(ctx context.Context, namespace string, activeUsers map[string]bool) error {
	if !c.detectedOnce {
		_ = c.DiscoverResourceNames(ctx, namespace)
	}

	c.mu.RLock()
	cmName := c.cmName
	c.mu.RUnlock()

	var cm corev1.ConfigMap
	if err := c.k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: cmName}, &cm); err != nil {
		return err
	}
	if cm.Data == nil {
		return nil
	}

	var usersToPrune []string
	for key := range cm.Data {
		if strings.HasPrefix(key, "accounts.") && !strings.HasSuffix(key, ".enabled") {
			parts := strings.Split(key, ".")
			if len(parts) == 2 {
				username := parts[1]
				if c.IsIgnoredUser(username) {
					continue
				}
				if !activeUsers[username] {
					usersToPrune = append(usersToPrune, username)
				}
			}
		}
	}

	for _, user := range usersToPrune {
		c.logger.Info("Pruning orphaned user", slog.String("username", user))
		if err := c.DeleteUser(ctx, namespace, user); err != nil {
			c.logger.Error("Failed to prune user", slog.String("username", user), slog.Any("error", err))
		}
	}

	return nil
}
