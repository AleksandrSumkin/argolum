# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.0] - 2026-11-22

### Fixed
- Fixed controller-runtime logger initialization (`ctrl.SetLogger`) via `logr.FromSlogHandler` to prevent `log.SetLogger(...) was never called` error and stack trace dump after 30 seconds ([#1](https://github.com/AleksandrSumkin/argolum/issues/1)).
- Added validation and normalization for `LOG_LEVEL` environment variable with warning fallback to INFO for invalid levels ([#1](https://github.com/AleksandrSumkin/argolum/issues/1)).
- Fixed Dockerfile builder stage `WORKDIR /app` to resolve Kaniko build context collision ([#1](https://github.com/AleksandrSumkin/argolum/issues/1)).

### Added
- Added comprehensive unit tests for `logging` package in `internal/logging/logger_test.go`.
- Added clean Helm chart for argolum operator in `deploy/helm/argolum` ([#3](https://github.com/AleksandrSumkin/argolum/issues/3)).
- Added official sidecar configuration in `manifests/sidecar-values.yaml` for ArgoCD integration.
- Added Dependabot configuration for automated dependency security updates.

## [0.1.0] - 2026-11-15

### Added
- Initial release of argolum (ArgoCD Local Users Manager).
- Automatic bcrypt password hashing and synchronization into `argocd-secret`.
- Declarative user account creation and status management in `argocd-cm`.
- Isolated RBAC policies management in `argocd-rbac-cm` (`policy.csv`).
- Finalizer support for clean user deletion upon Secret deletion.
- Background reconciler and orphan pruner.
