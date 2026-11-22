package controller

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/aleksandrsumkin/argolum/internal/argocd"
	"github.com/aleksandrsumkin/argolum/internal/config"
)

const (
	FinalizerName          = "argolum.argoproj.io/finalizer"
	AnnotationChecksum     = "argolum/checksum"
	AnnotationLastSyncTime = "argolum/last-sync-time"
	LabelManagedBy         = "app.kubernetes.io/managed-by"
	ManagedByValue         = "argolum"
)

// SecretReconciler reconciles Secret objects matching argolum labels.
type SecretReconciler struct {
	client.Client
	Cfg          *config.Config
	ArgoCDClient *argocd.Client
	Logger       *slog.Logger
}

// Reconcile performs the reconciliation loop for a user Secret.
func (r *SecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Logger.With(
		slog.String("secret", req.NamespacedName.String()),
	)

	var secret corev1.Secret
	if err := r.Get(ctx, req.NamespacedName, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error("Failed to get secret", slog.Any("error", err))
		return ctrl.Result{}, err
	}

	// Filter by namespace if WATCH_NAMESPACE is set
	if r.Cfg.WatchNamespace != "" && secret.Namespace != r.Cfg.WatchNamespace {
		return ctrl.Result{}, nil
	}

	// Verify labels match
	if !r.matchesLabels(&secret) {
		return ctrl.Result{}, nil
	}

	// Handle Deletion with Finalizer
	if !secret.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&secret, FinalizerName) {
			log.Info("Secret is being deleted, cleaning up ArgoCD configuration")
			username := r.extractUsername(&secret)
			if username != "" {
				if err := r.ArgoCDClient.DeleteUser(ctx, secret.Namespace, username); err != nil {
					log.Error("Failed to delete user from ArgoCD", slog.String("username", username), slog.Any("error", err))
					return ctrl.Result{}, err
				}
			}

			controllerutil.RemoveFinalizer(&secret, FinalizerName)
			if err := r.Update(ctx, &secret); err != nil {
				log.Error("Failed to remove finalizer from secret", slog.Any("error", err))
				return ctrl.Result{}, err
			}
			log.Info("Successfully removed finalizer and deleted user", slog.String("username", username))
		}
		return ctrl.Result{}, nil
	}

	// Parse user specification from secret
	spec, err := r.parseUserSpec(&secret)
	if err != nil {
		log.Warn("Invalid user secret specification", slog.Any("error", err))
		return ctrl.Result{}, nil
	}

	// Safeguard admin user
	if r.ArgoCDClient.IsIgnoredUser(spec.Username) {
		log.Warn("Ignoring attempt to manage protected user account", slog.String("username", spec.Username))
		return ctrl.Result{}, nil
	}

	// Check if already in sync
	checksum := argocd.CalculateChecksum(spec)
	currentChecksum := ""
	if secret.Annotations != nil {
		currentChecksum = secret.Annotations[AnnotationChecksum]
	}
	hasFinalizer := controllerutil.ContainsFinalizer(&secret, FinalizerName)
	hasManagedBy := secret.Labels != nil && secret.Labels[LabelManagedBy] == ManagedByValue

	if currentChecksum == checksum && hasFinalizer && hasManagedBy {
		log.Debug("Secret already in sync, skipping", slog.String("username", spec.Username))
		return ctrl.Result{}, nil
	}

	log.Info("Synchronizing user to ArgoCD", slog.String("username", spec.Username))

	// Reconcile user into ArgoCD ConfigMaps & Secret
	if err := r.ArgoCDClient.SyncUser(ctx, secret.Namespace, spec); err != nil {
		log.Error("Failed to sync user to ArgoCD", slog.String("username", spec.Username), slog.Any("error", err))
		return ctrl.Result{}, err
	}

	// Update metadata: Finalizer, Labels, Annotations
	controllerutil.AddFinalizer(&secret, FinalizerName)

	if secret.Labels == nil {
		secret.Labels = make(map[string]string)
	}
	secret.Labels[LabelManagedBy] = ManagedByValue

	if secret.Annotations == nil {
		secret.Annotations = make(map[string]string)
	}
	secret.Annotations[AnnotationChecksum] = checksum
	secret.Annotations[AnnotationLastSyncTime] = time.Now().UTC().Format(time.RFC3339)

	if err := r.Update(ctx, &secret); err != nil {
		log.Error("Failed to update secret annotations and labels", slog.Any("error", err))
		return ctrl.Result{}, err
	}

	log.Info("Secret reconciled successfully", slog.String("username", spec.Username), slog.String("checksum", checksum))
	return ctrl.Result{}, nil
}

func (r *SecretReconciler) matchesLabels(secret *corev1.Secret) bool {
	if secret.Labels == nil {
		return false
	}
	nameVal, nameOk := secret.Labels["app.kubernetes.io/name"]
	partOfVal, partOfOk := secret.Labels["app.kubernetes.io/part-of"]
	return nameOk && nameVal == r.Cfg.LabelName && partOfOk && partOfVal == r.Cfg.LabelPartOf
}

func (r *SecretReconciler) extractUsername(secret *corev1.Secret) string {
	if secret.Data != nil {
		if u, ok := secret.Data["username"]; ok && len(u) > 0 {
			return strings.TrimSpace(string(u))
		}
	}
	return ""
}

func (r *SecretReconciler) parseUserSpec(secret *corev1.Secret) (argocd.UserSpec, error) {
	username := r.extractUsername(secret)
	if username == "" {
		return argocd.UserSpec{}, fmt.Errorf("mandatory field 'username' is missing or empty")
	}

	password := ""
	if secret.Data != nil {
		if p, ok := secret.Data["password"]; ok {
			password = string(p)
		}
	}
	if password == "" {
		return argocd.UserSpec{}, fmt.Errorf("mandatory field 'password' is missing or empty")
	}

	enabled := true
	if secret.Data != nil {
		if e, ok := secret.Data["enabled"]; ok {
			b, err := strconv.ParseBool(strings.TrimSpace(string(e)))
			if err != nil {
				return argocd.UserSpec{}, fmt.Errorf("field 'enabled' must be boolean: %w", err)
			}
			enabled = b
		} else {
			return argocd.UserSpec{}, fmt.Errorf("mandatory field 'enabled' is missing")
		}
	}

	apiKey := true
	if secret.Data != nil {
		if a, ok := secret.Data["apiKey"]; ok {
			b, err := strconv.ParseBool(strings.TrimSpace(string(a)))
			if err != nil {
				return argocd.UserSpec{}, fmt.Errorf("field 'apiKey' must be boolean: %w", err)
			}
			apiKey = b
		} else {
			return argocd.UserSpec{}, fmt.Errorf("mandatory field 'apiKey' is missing")
		}
	}

	login := true
	if secret.Data != nil {
		if l, ok := secret.Data["login"]; ok {
			b, err := strconv.ParseBool(strings.TrimSpace(string(l)))
			if err != nil {
				return argocd.UserSpec{}, fmt.Errorf("field 'login' must be boolean: %w", err)
			}
			login = b
		} else {
			return argocd.UserSpec{}, fmt.Errorf("mandatory field 'login' is missing")
		}
	}

	roles := ""
	if secret.Data != nil {
		if ro, ok := secret.Data["roles"]; ok {
			roles = strings.TrimSpace(string(ro))
		}
	}

	return argocd.UserSpec{
		Username: username,
		Password: password,
		Enabled:  enabled,
		APIKey:   apiKey,
		Login:    login,
		Roles:    roles,
	}, nil
}

// SetupWithManager registers the SecretReconciler with the controller manager.
func (r *SecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}).
		WithEventFilter(predicate.NewPredicateFuncs(func(obj client.Object) bool {
			sec, ok := obj.(*corev1.Secret)
			if !ok {
				return false
			}
			return r.matchesLabels(sec)
		})).
		Complete(r)
}
