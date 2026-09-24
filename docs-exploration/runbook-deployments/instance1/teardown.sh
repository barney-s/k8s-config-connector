#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
source "${SCRIPT_DIR}/params.env"

echo "=== Tearing Down Config Connector Instance: ${RESOURCE_PREFIX} ==="
echo "Project:   ${PROJECT_ID}"
echo "Zone:      ${ZONE}"
echo "Cluster:   ${CLUSTER_NAME}"
echo "Namespace: ${KCC_NAMESPACE}"

# 1. Delete test GCP StorageBucket CR
echo "Deleting test StorageBucket ${TEST_BUCKET_NAME}..."
kubectl delete storagebucket "${TEST_BUCKET_NAME}" -n "${KCC_NAMESPACE}" --ignore-not-found=true --timeout=120s || true

# 2. Delete ConfigConnectorContext and ConfigConnector resources
echo "Deleting ConfigConnectorContext and ConfigConnector CRs..."
kubectl delete configconnectorcontext configconnectorcontext.core.cnrm.cloud.google.com -n "${KCC_NAMESPACE}" --ignore-not-found=true || true
kubectl delete configconnector configconnector.core.cnrm.cloud.google.com --ignore-not-found=true || true

# 3. Delete managed namespace
echo "Deleting namespace ${KCC_NAMESPACE}..."
kubectl delete namespace "${KCC_NAMESPACE}" --ignore-not-found=true || true

# 4. Uninstall Config Connector Operator
cd "${REPO_ROOT}"
echo "Uninstalling operator manifests..."
mkdir -p operator/config/manager
cp -f operator/config/manager/manager_image_patch_template.yaml operator/config/manager/manager_image_patch.yaml 2>/dev/null || true
sed -i'' -e "s@image: .*@image: ${OPERATOR_IMG}@" operator/config/manager/manager_image_patch.yaml 2>/dev/null || true
kubectl delete -k operator/config/default --ignore-not-found=true || true
rm -f operator/config/manager/manager_image_patch.yaml

# 5. Remove Workload Identity IAM binding and GSA
echo "Removing IAM policy bindings and deleting GSA ${KCC_GSA_EMAIL}..."
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

# 6. Delete GKE Cluster
echo "Deleting GKE cluster ${CLUSTER_NAME}..."
gcloud container clusters delete "${CLUSTER_NAME}" --zone="${ZONE}" --project="${PROJECT_ID}" --quiet || true

echo "=== Teardown Complete ==="
