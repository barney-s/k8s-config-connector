# Config Connector: GKE Deployment Runbook

## What this needs

Config Connector manages Google Cloud Platform (GCP) resources through Kubernetes Custom Resources. While unit and integration tests (such as `mockgcp` and envtest) run in-pod, **deploying Config Connector requires a real GKE cluster and GCP infrastructure**. This requirement is forced by:
1. **Workload Identity Federation**: KCC controller pods authenticate against Google Cloud IAM using GKE Workload Identity (`iam.gke.io/gcp-service-account` annotations and `roles/iam.workloadIdentityUser` IAM bindings mapped to the GKE cluster workload identity pool `${PROJECT_ID}.svc.id.goog`).
2. **GCP API Endpoints & Real Quota**: Actuating managed cloud infrastructure (e.g. Storage Buckets, IAM policies, networking) requires real network connectivity and valid IAM credentials against live GCP APIs.
3. **Cluster-level Webhooks and Custom Resource Definitions**: Admission and defaulting webhooks (`cnrm-webhook-manager`) require valid cluster certificates and Kubernetes API aggregation features.

### Verified Feasibility Checklist

Verified under active environment identity:
- [x] **`gcloud` CLI**: Present (`Google Cloud SDK 586.0.0`)
- [x] **`kubectl` CLI**: Present (`v1.35.8`)
- [x] **`go` toolchain**: Present (`go1.26.4`)
- [x] **GCP IAM - Project IAM Binding / Roles**: Granted (`roles/owner` / `roles/editor` on project)
- [x] **GCP GKE Cluster Access**: Granted (`container.clusters.list`, `container.clusters.getCredentials`)
- [ ] **Docker daemon**: MISSING in local pod (`docker: command not found`). When building custom operator images from source, either build inside a Docker-enabled environment via `make -C operator docker-build docker-push`, or submit image builds to Google Cloud Build using `gcloud builds submit --tag ${OPERATOR_IMG} -f operator/Dockerfile .`. Pre-built release images (e.g. `gcr.io/gke-release/cnrm/operator:${VERSION}`) can be deployed without a local Docker daemon.

### Teardown & Resource Costs
- **GKE Cluster**: Standard or Autopilot cluster node compute and control plane fees (~$0.10/hr per standard cluster plus VM instance charges).
- **GCP IAM Service Accounts**: Free of charge.
- **Actuated Test GCP Resources** (e.g. StorageBucket): Object storage and bucket operations cost (<$0.01 for test verification).
- **Teardown Command Runtime**: ~3–5 minutes.

---

## Preconditions

Ensure the following environment variables are set in your shell:

```bash
# GCP Project ID and Region/Zone
export PROJECT_ID="${PROJECT_ID:-$(gcloud config get-value project)}"
export REGION="${REGION:-us-central1}"
export ZONE="${ZONE:-us-central1-a}"

# GKE Cluster and Resource Identifier Prefix
export RESOURCE_PREFIX="${RESOURCE_PREFIX:-kcc-deploy}"
export CLUSTER_NAME="${CLUSTER_NAME:-${RESOURCE_PREFIX}-cluster}"

# Google Service Account (GSA) and Namespaces
export KCC_GSA_NAME="${RESOURCE_PREFIX}-sa"
export KCC_GSA_EMAIL="${KCC_GSA_NAME}@${PROJECT_ID}.iam.gserviceaccount.com"
export KCC_NAMESPACE="${KCC_NAMESPACE:-config-control}"
export OPERATOR_SYSTEM_NS="configconnector-operator-system"
export CNRM_SYSTEM_NS="cnrm-system"

# Operator Version to deploy (defaults to repo VERSION)
export VERSION="$(cat version/VERSION)"
export OPERATOR_IMG="gcr.io/gke-release/cnrm/operator:${VERSION}"
```

---

## Steps

### 1. Provision GKE Cluster with Workload Identity Enabled (if not already existing)

Workload Identity is required for KCC controllers to impersonate the Google Service Account without static keys.

```bash
# Check if cluster exists; create with Workload Identity if missing
if ! gcloud container clusters describe "${CLUSTER_NAME}" --zone="${ZONE}" --project="${PROJECT_ID}" >/dev/null 2>&1; then
  echo "Creating GKE cluster ${CLUSTER_NAME} with Workload Identity enabled..."
  gcloud container clusters create "${CLUSTER_NAME}" \
    --zone="${ZONE}" \
    --project="${PROJECT_ID}" \
    --workload-pool="${PROJECT_ID}.svc.id.goog" \
    --num-nodes=2 \
    --machine-type="e2-standard-4" \
    --release-channel="regular"
fi

# Fetch kubectl credentials for the cluster
gcloud container clusters get-credentials "${CLUSTER_NAME}" --zone="${ZONE}" --project="${PROJECT_ID}"
```

### 2. Create Google Service Account (GSA) and Assign IAM Permissions

KCC uses this GSA to manage GCP resources.

```bash
# Create the Google Service Account
if ! gcloud iam service-accounts describe "${KCC_GSA_EMAIL}" --project="${PROJECT_ID}" >/dev/null 2>&1; then
  gcloud iam service-accounts create "${KCC_GSA_NAME}" \
    --project="${PROJECT_ID}" \
    --display-name="Config Connector Service Account"
fi

# Grant the GSA permissions to manage resources on the project (e.g., roles/editor or specific admin roles)
gcloud projects add-iam-policy-binding "${PROJECT_ID}" \
  --member="serviceAccount:${KCC_GSA_EMAIL}" \
  --role="roles/editor"
```

### 3. Build Manifests and Deploy Config Connector Operator

Deploy the operator StatefulSet and CRDs using the repository's kustomize configuration.

```bash
# Prepare the operator manager image patch
mkdir -p operator/config/manager
cp operator/config/manager/manager_image_patch_template.yaml operator/config/manager/manager_image_patch.yaml
sed -i'' -e "s@image: .*@image: ${OPERATOR_IMG}@" operator/config/manager/manager_image_patch.yaml

# Generate operator manifests and apply via kubectl kustomize
make -C operator manifests
kubectl apply -k operator/config/default

# Clean up local image patch artifact
rm -f operator/config/manager/manager_image_patch.yaml

# Wait for operator to become ready
kubectl wait -n "${OPERATOR_SYSTEM_NS}" --for=jsonpath='{.status.readyReplicas}'=1 statefulset/configconnector-operator --timeout=300s
```

### 4. Configure Config Connector Mode (Namespaced Mode with Workload Identity)

Namespaced mode is the recommended architecture for Config Connector.

```bash
# Create the cluster-wide ConfigConnector configuration in namespaced mode
cat <<EOF | kubectl apply -f -
apiVersion: core.cnrm.cloud.google.com/v1beta1
kind: ConfigConnector
metadata:
  name: configconnector.core.cnrm.cloud.google.com
spec:
  mode: namespaced
  stateIntoSpec: Absent
EOF

# Create the managed namespace
kubectl create namespace "${KCC_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

# Configure ConfigConnectorContext in the managed namespace
cat <<EOF | kubectl apply -f -
apiVersion: core.cnrm.cloud.google.com/v1beta1
kind: ConfigConnectorContext
metadata:
  name: configconnectorcontext.core.cnrm.cloud.google.com
  namespace: ${KCC_NAMESPACE}
spec:
  googleServiceAccount: "${KCC_GSA_EMAIL}"
  stateIntoSpec: Absent
EOF
```

### 5. Bind Workload Identity (K8s ServiceAccount to Google ServiceAccount)

In namespaced mode, the operator deploys a per-namespace controller manager using the Kubernetes ServiceAccount `cnrm-controller-manager-${KCC_NAMESPACE}` in the `cnrm-system` namespace.

```bash
# Authorize the K8s ServiceAccount to impersonate the Google Service Account via Workload Identity User role
K8S_SA="serviceAccount:${PROJECT_ID}.svc.id.goog[${CNRM_SYSTEM_NS}/cnrm-controller-manager-${KCC_NAMESPACE}]"

gcloud iam service-accounts add-iam-policy-binding "${KCC_GSA_EMAIL}" \
  --project="${PROJECT_ID}" \
  --role="roles/iam.workloadIdentityUser" \
  --member="${K8S_SA}"
```

---

## Verify

### 1. Verify Operator and System Pods Status

```bash
kubectl get pods -n "${OPERATOR_SYSTEM_NS}"
kubectl get pods -n "${CNRM_SYSTEM_NS}"
```

### 2. Verify ConfigConnector and ConfigConnectorContext Health

```bash
kubectl wait --for=condition=Healthy=true configconnector/configconnector.core.cnrm.cloud.google.com --timeout=180s
kubectl wait -n "${KCC_NAMESPACE}" --for=condition=Healthy=true configconnectorcontext/configconnectorcontext.core.cnrm.cloud.google.com --timeout=180s
```

### 3. Verify End-to-End Resource Reconciliation via Workload Identity

Create a test `StorageBucket` resource in the managed namespace and verify it reaches `Ready` status on GCP.

```bash
TEST_BUCKET_NAME="${RESOURCE_PREFIX}-bucket-$(date +%s)"

cat <<EOF | kubectl apply -f -
apiVersion: storage.cnrm.cloud.google.com/v1beta1
kind: StorageBucket
metadata:
  name: ${TEST_BUCKET_NAME}
  namespace: ${KCC_NAMESPACE}
  annotations:
    cnrm.cloud.google.com/project-id: "${PROJECT_ID}"
spec:
  location: "${REGION}"
  uniformBucketLevelAccess: true
  lifecycleRule:
    - action:
        type: Delete
      condition:
        age: 1
        withState: ANY
EOF

# Wait for KCC to reconcile the resource
kubectl wait -n "${KCC_NAMESPACE}" --for=condition=Ready=true storagebucket/${TEST_BUCKET_NAME} --timeout=180s

# Confirm bucket exists in GCP Cloud Storage
gcloud storage buckets describe "gs://${TEST_BUCKET_NAME}" --project="${PROJECT_ID}"
```

---

## Teardown

Clean up all test resources, namespaces, operator manifests, and IAM bindings.

```bash
# 1. Delete the test GCP StorageBucket CR
kubectl delete storagebucket "${TEST_BUCKET_NAME}" -n "${KCC_NAMESPACE}" --ignore-not-found=true --timeout=120s

# 2. Delete ConfigConnectorContext and ConfigConnector resources
kubectl delete configconnectorcontext configconnectorcontext.core.cnrm.cloud.google.com -n "${KCC_NAMESPACE}" --ignore-not-found=true
kubectl delete configconnector configconnector.core.cnrm.cloud.google.com --ignore-not-found=true

# 3. Delete managed namespace
kubectl delete namespace "${KCC_NAMESPACE}" --ignore-not-found=true

# 4. Uninstall Config Connector Operator
kubectl delete -k operator/config/default --ignore-not-found=true

# 5. Remove Workload Identity IAM binding and GSA
gcloud iam service-accounts remove-iam-policy-binding "${KCC_GSA_EMAIL}" \
  --project="${PROJECT_ID}" \
  --role="roles/iam.workloadIdentityUser" \
  --member="${K8S_SA}" \
  --quiet || true

gcloud projects remove-iam-policy-binding "${PROJECT_ID}" \
  --member="serviceAccount:${KCC_GSA_EMAIL}" \
  --role="roles/editor" \
  --quiet || true

gcloud iam service-accounts delete "${KCC_GSA_EMAIL}" --project="${PROJECT_ID}" --quiet || true

# 6. Delete GKE Cluster (if created specifically for this runbook)
# gcloud container clusters delete "${CLUSTER_NAME}" --zone="${ZONE}" --project="${PROJECT_ID}" --quiet
```
