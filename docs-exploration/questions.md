# Open Questions

Living record of architectural or operational ambiguities encountered in the repository.

1. **Autopilot vs Standard Channel Manifest Packaging**:
   `operator/channels` and `operator/autopilot-channels` maintain distinct per-version manifests. While standard manifests include sidecars like `prometheus-to-sd`, autopilot channels use tailored resource allocations. Should the GKE deployment runbook offer an automated flag to target Autopilot channels directly via `operator/config/autopilot` instead of `operator/config/default`?

2. **Workload Identity Namespace Mapping Strategy**:
   The operator uses `cnrm-system/cnrm-controller-manager-${NAMESPACE}` for namespaced mode by default unless `spec.managerNamespace` is explicitly set in `ConfigConnectorContext`. For multi-tenant clusters with strict RBAC segregation, should the runbook document dedicated `managerNamespace` isolation patterns?
