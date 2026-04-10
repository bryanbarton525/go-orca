# QA Checklist — Kubernetes / GitOps

Use this checklist when reviewing changes under `clusters/` or any Kubernetes manifest.

## Manifest Safety

- [ ] No `kubectl apply` of unreviewed manifests — all changes go through GitOps (ArgoCD)
- [ ] Namespace explicitly set on all resources
- [ ] RBAC: least-privilege; no `ClusterAdmin` for application service accounts
- [ ] NetworkPolicy defined for sensitive namespaces

## Secrets

- [ ] No plaintext secrets committed; use Sealed Secrets or reference an external secret store
- [ ] Sealed Secret controller namespace matches the target namespace's expected controller
- [ ] Old sealed secrets rotated after credential rotation

## Deployment Config

- [ ] `replicas` ≥2 for production workloads (unless StatefulSet with anti-affinity)
- [ ] `resources.requests` and `resources.limits` set on every container
- [ ] `livenessProbe` and `readinessProbe` configured; `startupProbe` for slow-starting services
- [ ] `imagePullPolicy: Always` only for mutable tags; prefer digest-pinned images
- [ ] `PodDisruptionBudget` defined for HA workloads

## Kustomize / ArgoCD

- [ ] `kustomization.yaml` references are valid (`kubeval` or `kustomize build` passes)
- [ ] ArgoCD app target namespace matches the manifest namespace
- [ ] Sync policy: automated sync is intentional; manual for sensitive workloads
- [ ] No drift between the ArgoCD app spec and the Git source of truth

## Rollout

- [ ] Migration steps (DB schema, config changes) documented in the PR description
- [ ] Rollback plan identified if the change cannot be reverted declaratively
- [ ] `affected_namespaces` listed in the PR description
