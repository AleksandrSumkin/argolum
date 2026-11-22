package controller

import (
	"context"
	"log/slog"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/aleksandrsumkin/argolum/internal/argocd"
	"github.com/aleksandrsumkin/argolum/internal/config"
)

// Pruner periodically checks for orphaned local users in ArgoCD configurations and deletes them.
type Pruner struct {
	Client       client.Client
	Cfg          *config.Config
	ArgoCDClient *argocd.Client
	Logger       *slog.Logger
}

// Start runs the periodic pruning loop until ctx is canceled.
func (p *Pruner) Start(ctx context.Context) error {
	if !p.Cfg.PruneOrphanUsers {
		p.Logger.Info("Pruning of orphan users is disabled")
		<-ctx.Done()
		return nil
	}

	p.Logger.Info("Starting periodic orphan user pruner",
		slog.Duration("interval", p.Cfg.PruneInterval),
		slog.String("namespace", p.Cfg.WatchNamespace),
	)

	ticker := time.NewTicker(p.Cfg.PruneInterval)
	defer ticker.Stop()

	// Run initial prune pass shortly after startup
	select {
	case <-time.After(15 * time.Second):
		p.runPrune(ctx)
	case <-ctx.Done():
		return nil
	}

	for {
		select {
		case <-ticker.C:
			p.runPrune(ctx)
		case <-ctx.Done():
			p.Logger.Info("Stopping orphan user pruner")
			return nil
		}
	}
}

func (p *Pruner) runPrune(ctx context.Context) {
	p.Logger.Debug("Running orphan user prune pass")

	listOpts := []client.ListOption{
		client.MatchingLabels{
			"app.kubernetes.io/name":    p.Cfg.LabelName,
			"app.kubernetes.io/part-of": p.Cfg.LabelPartOf,
		},
	}
	if p.Cfg.WatchNamespace != "" {
		listOpts = append(listOpts, client.InNamespace(p.Cfg.WatchNamespace))
	}

	var secretList corev1.SecretList
	if err := p.Client.List(ctx, &secretList, listOpts...); err != nil {
		p.Logger.Error("Failed to list user secrets during prune", slog.Any("error", err))
		return
	}

	activeUsers := make(map[string]bool)
	for _, sec := range secretList.Items {
		if !sec.DeletionTimestamp.IsZero() {
			continue
		}
		if sec.Data != nil {
			if u, ok := sec.Data["username"]; ok && len(u) > 0 {
				activeUsers[strings.TrimSpace(string(u))] = true
			}
		}
	}

	ns := p.Cfg.WatchNamespace
	if ns == "" {
		ns = "argocd"
	}

	if err := p.ArgoCDClient.PruneUsers(ctx, ns, activeUsers); err != nil {
		p.Logger.Error("Failed to execute prune pass", slog.Any("error", err))
	}
}
