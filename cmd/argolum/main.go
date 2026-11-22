package main

import (
	"log/slog"
	"os"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/aleksandrsumkin/argolum/internal/argocd"
	"github.com/aleksandrsumkin/argolum/internal/config"
	"github.com/aleksandrsumkin/argolum/internal/controller"
	"github.com/aleksandrsumkin/argolum/internal/logging"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
}

func main() {
	cfg := config.Load()
	logger := logging.Setup(cfg.LogLevel, cfg.LogFormat)
	ctrl.SetLogger(logr.FromSlogHandler(logger.Handler()))

	logger.Info("Starting argolum (ArgoCD Local Users Manager)",
		slog.String("watchNamespace", cfg.WatchNamespace),
		slog.Bool("pruneOrphans", cfg.PruneOrphanUsers),
		slog.String("logLevel", cfg.LogLevel),
	)

	mgrOpts := ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: cfg.MetricsBindAddr,
		},
		HealthProbeBindAddress: cfg.HealthProbeBindAddr,
	}

	if cfg.WatchNamespace != "" {
		mgrOpts.Cache = cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				cfg.WatchNamespace: {},
			},
		}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), mgrOpts)
	if err != nil {
		logger.Error("Unable to start manager", slog.Any("error", err))
		os.Exit(1)
	}

	argocdClient := argocd.NewClient(mgr.GetClient(), cfg, logger)

	secretReconciler := &controller.SecretReconciler{
		Client:       mgr.GetClient(),
		Cfg:          cfg,
		ArgoCDClient: argocdClient,
		Logger:       logger,
	}
	if err := secretReconciler.SetupWithManager(mgr); err != nil {
		logger.Error("Unable to create Secret controller", slog.Any("error", err))
		os.Exit(1)
	}

	if cfg.PruneOrphanUsers {
		pruner := &controller.Pruner{
			Client:       mgr.GetClient(),
			Cfg:          cfg,
			ArgoCDClient: argocdClient,
			Logger:       logger,
		}
		if err := mgr.Add(pruner); err != nil {
			logger.Error("Unable to add Pruner runnable", slog.Any("error", err))
			os.Exit(1)
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		logger.Error("Unable to set up health check", slog.Any("error", err))
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		logger.Error("Unable to set up ready check", slog.Any("error", err))
		os.Exit(1)
	}

	logger.Info("Starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error("Problem running manager", slog.Any("error", err))
		os.Exit(1)
	}
}
