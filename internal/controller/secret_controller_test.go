package controller

import (
	"context"
	"io"
	"log/slog"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/aleksandrsumkin/argolum/internal/argocd"
	"github.com/aleksandrsumkin/argolum/internal/config"
)

func TestSecretReconciler_FullLifecycle(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "argocd",
			Name:      "argocd-cm",
		},
		Data: map[string]string{},
	}
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "argocd",
			Name:      "argocd-secret",
		},
		Data: map[string][]byte{},
	}
	rbacCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "argocd",
			Name:      "argocd-rbac-cm",
		},
		Data: map[string]string{
			"policy.csv": "",
		},
	}

	userSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "argocd",
			Name:      "argocd-local-user-jenkins",
			Labels: map[string]string{
				"app.kubernetes.io/name":    "argolum",
				"app.kubernetes.io/part-of": "argocd",
			},
		},
		Data: map[string][]byte{
			"username": []byte("jenkins"),
			"password": []byte("pass123!"),
			"enabled":  []byte("true"),
			"apiKey":   []byte("true"),
			"login":    []byte("true"),
			"roles":    []byte("proj:default:admin"),
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cm, sec, rbacCM, userSecret).
		Build()

	cfg := &config.Config{
		WatchNamespace:   "argocd",
		ArgocdCMName:     "argocd-cm",
		ArgocdSecretName: "argocd-secret",
		ArgocdRBACCMName: "argocd-rbac-cm",
		LabelName:        "argolum",
		LabelPartOf:      "argocd",
		BcryptCost:       4,
		IgnoreUsers:      []string{"admin"},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	argoClient := argocd.NewClient(fakeClient, cfg, logger)

	reconciler := &SecretReconciler{
		Client:       fakeClient,
		Cfg:          cfg,
		ArgoCDClient: argoClient,
		Logger:       logger,
	}

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "argocd",
			Name:      "argocd-local-user-jenkins",
		},
	}

	// 1. Initial Reconcile
	res, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}
	if res.Requeue {
		t.Fatalf("Reconcile should not requeue")
	}

	// Verify User Secret was annotated & labeled
	var updatedSec corev1.Secret
	if err := fakeClient.Get(ctx, req.NamespacedName, &updatedSec); err != nil {
		t.Fatalf("Failed to fetch updated user secret: %v", err)
	}
	if updatedSec.Labels[LabelManagedBy] != ManagedByValue {
		t.Errorf("Missing label %s", LabelManagedBy)
	}
	if updatedSec.Annotations[AnnotationChecksum] == "" {
		t.Errorf("Missing annotation %s", AnnotationChecksum)
	}
	if updatedSec.Annotations[AnnotationLastSyncTime] == "" {
		t.Errorf("Missing annotation %s", AnnotationLastSyncTime)
	}

	// Verify ArgoCD CM updated
	var updatedCM corev1.ConfigMap
	_ = fakeClient.Get(ctx, types.NamespacedName{Namespace: "argocd", Name: "argocd-cm"}, &updatedCM)
	if updatedCM.Data["accounts.jenkins"] != "apiKey, login" {
		t.Errorf("accounts.jenkins was not set: %s", updatedCM.Data["accounts.jenkins"])
	}

	// 2. Second Reconcile (No-op)
	res2, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Second Reconcile failed: %v", err)
	}
	if res2.Requeue {
		t.Fatalf("Second Reconcile should not requeue")
	}

	// 3. Deletion with Finalizer
	if err := fakeClient.Delete(ctx, &updatedSec); err != nil {
		t.Fatalf("Failed to delete user secret: %v", err)
	}

	res3, err := reconciler.Reconcile(ctx, req)
	if err != nil {
		t.Fatalf("Deletion Reconcile failed: %v", err)
	}
	if res3.Requeue {
		t.Fatalf("Deletion Reconcile should not requeue")
	}

	// Verify cleaned up from ArgoCD CM
	var finalCM corev1.ConfigMap
	_ = fakeClient.Get(ctx, types.NamespacedName{Namespace: "argocd", Name: "argocd-cm"}, &finalCM)
	if _, exists := finalCM.Data["accounts.jenkins"]; exists {
		t.Errorf("accounts.jenkins should have been deleted")
	}
}
