TORN-DOWN

## Summary

Executed `docs-exploration/agent-runs/instance1/teardown.sh` deterministically to remove all resources created for `kcc-instance1` in project `barni-cnrm-20260529`. All components were cleanly deleted and confirmed absent via live GCP queries.

---

## Removed Resources & Verification Evidence

### 1. Test GCP StorageBucket (`kcc-instance1-bucket`)
- **Action**: Deleted `StorageBucket` CR from Kubernetes namespace `kcc-instance1-control`; verified underlying GCS bucket deletion in GCP Cloud Storage.
- **Verification Evidence**:
  ```text
  $ gcloud storage buckets describe gs://kcc-instance1-bucket --project=barni-cnrm-20260529
  ERROR: (gcloud.storage.buckets.describe) gs://kcc-instance1-bucket not found: 404.
  ```

### 2. Config Connector Core Resources & Managed Namespace
- **Action**: Deleted `ConfigConnectorContext` (`kcc-instance1-control`), `ConfigConnector` (`configconnector.core.cnrm.cloud.google.com`), and namespace `kcc-instance1-control`.
- **Verification Evidence**:
  ```text
  configconnectorcontext.core.cnrm.cloud.google.com "configconnectorcontext.core.cnrm.cloud.google.com" deleted from kcc-instance1-control namespace
  configconnector.core.cnrm.cloud.google.com "configconnector.core.cnrm.cloud.google.com" deleted
  namespace "kcc-instance1-control" deleted
  ```

### 3. Config Connector Operator & CRDs
- **Action**: Deleted operator StatefulSet, RBAC, service, and CRDs via `kubectl delete -k operator/config/default`.
- **Verification Evidence**:
  ```text
  namespace "configconnector-operator-system" deleted
  customresourcedefinition.apiextensions.k8s.io "configconnectorcontexts.core.cnrm.cloud.google.com" deleted
  customresourcedefinition.apiextensions.k8s.io "configconnectors.core.cnrm.cloud.google.com" deleted
  customresourcedefinition.apiextensions.k8s.io "controllerreconcilers.customize.core.cnrm.cloud.google.com" deleted
  customresourcedefinition.apiextensions.k8s.io "controllerresources.customize.core.cnrm.cloud.google.com" deleted
  customresourcedefinition.apiextensions.k8s.io "mutatingwebhookconfigurationcustomizations.customize.core.cnrm.cloud.google.com" deleted
  customresourcedefinition.apiextensions.k8s.io "namespacedcontrollerreconcilers.customize.core.cnrm.cloud.google.com" deleted
  customresourcedefinition.apiextensions.k8s.io "namespacedcontrollerresources.customize.core.cnrm.cloud.google.com" deleted
  customresourcedefinition.apiextensions.k8s.io "validatingwebhookconfigurationcustomizations.customize.core.cnrm.cloud.google.com" deleted
  serviceaccount "configconnector-operator" deleted
  clusterrole.rbac.authorization.k8s.io "configconnector-operator-cnrm-viewer" deleted
  clusterrole.rbac.authorization.k8s.io "configconnector-operator-manager-role" deleted
  clusterrolebinding.rbac.authorization.k8s.io "configconnector-operator-cnrm-viewer-role-binding" deleted
  clusterrolebinding.rbac.authorization.k8s.io "configconnector-operator-rolebinding" deleted
  service "configconnector-operator-service" deleted
  statefulset.apps "configconnector-operator" deleted
  ```

### 4. Google Service Account & IAM Bindings
- **Action**: Removed `roles/iam.workloadIdentityUser` binding on GSA `kcc-instance1-sa@barni-cnrm-20260529.iam.gserviceaccount.com`, removed `roles/editor` project binding, and deleted the GSA.
- **Verification Evidence**:
  ```text
  $ gcloud iam service-accounts describe kcc-instance1-sa@barni-cnrm-20260529.iam.gserviceaccount.com --project=barni-cnrm-20260529
  ERROR: (gcloud.iam.service-accounts.describe) PERMISSION_DENIED: Permission 'iam.serviceAccounts.get' denied on resource (or it may not exist).

  $ gcloud iam service-accounts list --project=barni-cnrm-20260529
  DISPLAY NAME                            EMAIL                                                               DISABLED
  Gemini API Key                          ais-gemini-key-8635a4ec499c49b@77658989016.iam.gserviceaccount.com  False
  Compute Engine default service account  77658989016-compute@developer.gserviceaccount.com                   False
  ```

### 5. GKE Cluster (`kcc-instance1-cluster`)
- **Action**: Deleted GKE cluster `kcc-instance1-cluster` in zone `us-central1-a`.
- **Verification Evidence**:
  ```text
  $ gcloud container clusters list --project=barni-cnrm-20260529
  NAME                LOCATION       MASTER_VERSION      MASTER_IP      MACHINE_TYPE   NODE_VERSION        NUM_NODES  STATUS    STACK_TYPE
  ax-instance5        us-central1-a  1.35.8-gke.1036000  34.45.168.90   c3-standard-4  1.35.8-gke.1036000  2          STOPPING  IPV4
  substrat-instance1  us-central1-a  1.36.4-gke.1391000  35.226.195.58  c3-standard-4  1.36.4-gke.1391000  2          STOPPING  IPV4
  ```

### 6. Lingering Disks / Instances / Stragglers
- **Action**: Checked for compute instances and persistent disks with name prefix or label `kcc-instance1`.
- **Verification Evidence**:
  ```text
  $ gcloud compute instances list --project=barni-cnrm-20260529
  Listed 0 items.

  $ gcloud compute disks list --project=barni-cnrm-20260529 --filter="name ~ kcc-instance1"
  Listed 0 items.
  ```

---

## What Remains

None. All resources owned by `instance1` (`kcc-instance1`) have been completely deleted.
