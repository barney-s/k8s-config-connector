TORN-DOWN

## Summary
The teardown procedure for run `gke1` was executed via `teardown.sh`. All provisioned resources (KRM test resources, Config Connector CRDs and manifests, GKE cluster, Google Service Account, IAM bindings, Artifact Registry repository, and temporary local patch files) were successfully deleted. No straggler resources remain in GCP.

## Verifying Evidence

### 1. Test KRM and GCP Pub/Sub Topic
- `pubsubtopic/kcc-gke1-test-topic` and namespace `kcc-gke1-test` were deleted.
- GCP Pub/Sub topic verification:
  ```
  $ gcloud pubsub topics list --project=barni-cnrm-20260529
  (0 topics matching kcc-gke1-test-topic returned)
  ```

### 2. Config Connector Manifests & CRDs
- All CRDs under `operator/config/crd/bases/` and `config/crds/resources/` were uninstalled.
- Namespace `cnrm-system` and all controller deployments/statefulsets/services/RBAC bindings were removed.

### 3. GKE Cluster
- GKE Cluster `kcc-gke1-cluster` in `us-central1-a` was deleted.
- Cluster verification:
  ```
  $ gcloud container clusters list --project=barni-cnrm-20260529
  Listed 0 items.
  ```

### 4. IAM & Google Service Account
- Project-level IAM policy binding (`roles/owner`) for `kcc-gke1-sa@barni-cnrm-20260529.iam.gserviceaccount.com` was removed.
- GSA `kcc-gke1-sa@barni-cnrm-20260529.iam.gserviceaccount.com` was deleted.
- Service account list verification:
  ```
  $ gcloud iam service-accounts list --project=barni-cnrm-20260529
  DISPLAY NAME                            EMAIL                                                               DISABLED
  Gemini API Key                          ais-gemini-key-8635a4ec499c49b@77658989016.iam.gserviceaccount.com  False
  Compute Engine default service account  77658989016-compute@developer.gserviceaccount.com                   False
  ```

### 5. Artifact Registry Repository
- Repository `kcc-gke1-repo` in `us-central1` was deleted.
- Artifact Registry verification:
  ```
  $ gcloud artifacts repositories list --project=barni-cnrm-20260529 --location=us-central1
  Listed 0 items.
  ```

### 6. Local Workspace
- Local generated patch files cleaned up; working tree clean.

## Left Running
None. All resources associated with run `gke1` have been torn down.
