#!/usr/bin/env bash
set -euo pipefail

# Note: Added retry loop for IAM propagation and updated health wait to use jsonpath='{.status.healthy}'=true.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
source "${SCRIPT_DIR}/params.env"

echo "=== Deploying Config Connector Instance: ${RESOURCE_PREFIX} ==="
echo "Project:   ${PROJECT_ID}"
echo "Zone:      ${ZONE}"
echo "Cluster:   ${CLUSTER_NAME}"
echo "Namespace: ${KCC_NAMESPACE}"

# 1. Provision GKE Cluster with Workload Identity Enabled
if ! gcloud container clusters describe "${CLUSTER_NAME}" --zone="${ZONE}" --project="${PROJECT_ID}" >/dev/null 2>&1; then
  echo "Provisioning GKE cluster ${CLUSTER_NAME} with Workload Identity..."
  gcloud container clusters create "${CLUSTER_NAME}" \
    --zone="${ZONE}" \
    --project="${PROJECT_ID}" \
    --workload-pool="${PROJECT_ID}.svc.id.goog" \
    --num-nodes=2 \
    --machine-type="e2-standard-4" \
    --release-channel="regular" \
    --labels="repo-agent-instance=${RESOURCE_PREFIX}"
else
  echo "GKE cluster ${CLUSTER_NAME} already exists."
fi

echo "Fetching cluster credentials..."
gcloud container clusters get-credentials "${CLUSTER_NAME}" --zone="${ZONE}" --project="${PROJECT_ID}"

# 2. Create Google Service Account and Assign IAM Permissions
if ! gcloud iam service-accounts describe "${KCC_GSA_EMAIL}" --project="${PROJECT_ID}" >/dev/null 2>&1; then
  echo "Creating Google Service Account ${KCC_GSA_NAME}..."
  gcloud iam service-accounts create "${KCC_GSA_NAME}" \
    --project="${PROJECT_ID}" \
    --display-name="Config Connector Service Account (${RESOURCE_PREFIX})" \
    --description="repo-agent-instance=${RESOURCE_PREFIX}"
else
  echo "Google Service Account ${KCC_GSA_NAME} already exists."
fi

echo "Assigning roles/editor to ${KCC_GSA_EMAIL} on project ${PROJECT_ID}..."
for i in {1..6}; do
  if gcloud projects add-iam-policy-binding "${PROJECT_ID}" \
    --member="serviceAccount:${KCC_GSA_EMAIL}" \
    --role="roles/editor"; then
    break
  fi
  echo "Retrying project IAM binding in 5s (attempt $i/6)..."
  sleep 5
done

# 3. Build Manifests and Deploy Config Connector Operator
cd "${REPO_ROOT}"
echo "Preparing operator manager image patch (${OPERATOR_IMG})..."
mkdir -p operator/config/manager
cp operator/config/manager/manager_image_patch_template.yaml operator/config/manager/manager_image_patch.yaml
sed -i'' -e "s@image: .*@image: ${OPERATOR_IMG}@" operator/config/manager/manager_image_patch.yaml

echo "Generating operator manifests and applying..."
make -C operator manifests
kubectl apply -k operator/config/default

rm -f operator/config/manager/manager_image_patch.yaml

echo "Waiting for operator to become ready..."
kubectl wait -n "${OPERATOR_SYSTEM_NS}" --for=jsonpath='{.status.readyReplicas}'=1 statefulset/configconnector-operator --timeout=300s

# 4. Configure Config Connector Mode (Namespaced Mode with Workload Identity)
echo "Configuring cluster-wide ConfigConnector resource in namespaced mode..."
cat <<EOF | kubectl apply -f -
apiVersion: core.cnrm.cloud.google.com/v1beta1
kind: ConfigConnector
metadata:
  name: configconnector.core.cnrm.cloud.google.com
spec:
  mode: namespaced
  stateIntoSpec: Absent
EOF

echo "Creating managed namespace ${KCC_NAMESPACE}..."
kubectl create namespace "${KCC_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

echo "Configuring ConfigConnectorContext in ${KCC_NAMESPACE}..."
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

# 5. Bind Workload Identity (K8s ServiceAccount to Google ServiceAccount)
echo "Authorizing K8s ServiceAccount via Workload Identity..."
for i in {1..6}; do
  if gcloud iam service-accounts add-iam-policy-binding "${KCC_GSA_EMAIL}" \
    --project="${PROJECT_ID}" \
    --role="roles/iam.workloadIdentityUser" \
    --member="${K8S_SA}"; then
    break
  fi
  echo "Retrying workload identity binding in 5s (attempt $i/6)..."
  sleep 5
done

# Verification
echo "=== Verifying Deployment ==="
echo "Operator and system pods:"
kubectl get pods -n "${OPERATOR_SYSTEM_NS}"
kubectl get pods -n "${CNRM_SYSTEM_NS}"

echo "Waiting for ConfigConnector and ConfigConnectorContext health..."
kubectl wait --for=jsonpath='{.status.healthy}'=true configconnector/configconnector.core.cnrm.cloud.google.com --timeout=180s
kubectl wait -n "${KCC_NAMESPACE}" --for=jsonpath='{.status.healthy}'=true configconnectorcontext/configconnectorcontext.core.cnrm.cloud.google.com --timeout=180s

echo "Creating test StorageBucket ${TEST_BUCKET_NAME} in ${KCC_NAMESPACE}..."
cat <<EOF | kubectl apply -f -
apiVersion: storage.cnrm.cloud.google.com/v1beta1
kind: StorageBucket
metadata:
  name: ${TEST_BUCKET_NAME}
  namespace: ${KCC_NAMESPACE}
  annotations:
    cnrm.cloud.google.com/project-id: "${PROJECT_ID}"
  labels:
    repo-agent-instance: "${RESOURCE_PREFIX}"
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

echo "Waiting for StorageBucket ${TEST_BUCKET_NAME} to become Ready..."
kubectl wait -n "${KCC_NAMESPACE}" --for=condition=Ready=true storagebucket/${TEST_BUCKET_NAME} --timeout=180s

echo "Confirming bucket exists in GCP Cloud Storage..."
gcloud storage buckets describe "gs://${TEST_BUCKET_NAME}" --project="${PROJECT_ID}"

echo "=== Deployment and Verification Successful ==="
