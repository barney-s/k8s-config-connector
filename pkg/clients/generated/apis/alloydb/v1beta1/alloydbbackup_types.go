// Copyright 2020 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// ----------------------------------------------------------------------------
//
//     ***     AUTO GENERATED CODE    ***    AUTO GENERATED CODE     ***
//
// ----------------------------------------------------------------------------
//
//     This file is automatically generated by Config Connector and manual
//     changes will be clobbered when the file is regenerated.
//
// ----------------------------------------------------------------------------

// *** DISCLAIMER ***
// Config Connector's go-client for CRDs is currently in ALPHA, which means
// that future versions of the go-client may include breaking changes.
// Please try it out and give us feedback!

package v1beta1

import (
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/clients/generated/apis/k8s/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type BackupEncryptionConfig struct {
	/* Immutable. The fully-qualified resource name of the KMS key. Each Cloud KMS key is regionalized and has the following format: projects/[PROJECT]/locations/[REGION]/keyRings/[RING]/cryptoKeys/[KEY_NAME]. */
	// +optional
	KmsKeyName *string `json:"kmsKeyName,omitempty"`
}

type AlloyDBBackupSpec struct {
	/* The full resource name of the backup source cluster (e.g., projects/{project}/locations/{location}/clusters/{clusterId}). */
	ClusterNameRef v1alpha1.ResourceRef `json:"clusterNameRef"`

	/* Immutable. User-provided description of the backup. */
	// +optional
	Description *string `json:"description,omitempty"`

	/* EncryptionConfig describes the encryption config of a cluster or a backup that is encrypted with a CMEK (customer-managed encryption key). */
	// +optional
	EncryptionConfig *BackupEncryptionConfig `json:"encryptionConfig,omitempty"`

	/* Immutable. The location where the alloydb backup should reside. */
	Location string `json:"location"`

	/* The project that this resource belongs to. */
	ProjectRef v1alpha1.ResourceRef `json:"projectRef"`

	/* Immutable. Optional. The backupId of the resource. Used for creation and acquisition. When unset, the value of `metadata.name` is used as the default. */
	// +optional
	ResourceID *string `json:"resourceID,omitempty"`
}

type BackupEncryptionInfoStatus struct {
	/* Output only. Type of encryption. */
	// +optional
	EncryptionType *string `json:"encryptionType,omitempty"`

	/* Output only. Cloud KMS key versions that are being used to protect the database or the backup. */
	// +optional
	KmsKeyVersions []string `json:"kmsKeyVersions,omitempty"`
}

type AlloyDBBackupStatus struct {
	/* Conditions represent the latest available observations of the
	   AlloyDBBackup's current state. */
	Conditions []v1alpha1.Condition `json:"conditions,omitempty"`
	/* Time the Backup was created in UTC. */
	// +optional
	CreateTime *string `json:"createTime,omitempty"`

	/* EncryptionInfo describes the encryption information of a cluster or a backup. */
	// +optional
	EncryptionInfo []BackupEncryptionInfoStatus `json:"encryptionInfo,omitempty"`

	/* A hash of the resource. */
	// +optional
	Etag *string `json:"etag,omitempty"`

	/* Output only. The name of the backup resource with the format: * projects/{project}/locations/{region}/backups/{backupId}. */
	// +optional
	Name *string `json:"name,omitempty"`

	/* ObservedGeneration is the generation of the resource that was most recently observed by the Config Connector controller. If this is equal to metadata.generation, then that means that the current reported status reflects the most recent desired state of the resource. */
	// +optional
	ObservedGeneration *int `json:"observedGeneration,omitempty"`

	/* If true, indicates that the service is actively updating the resource. This can happen due to user-triggered updates or system actions like failover or maintenance. */
	// +optional
	Reconciling *bool `json:"reconciling,omitempty"`

	/* The current state of the backup. */
	// +optional
	State *string `json:"state,omitempty"`

	/* Output only. The system-generated UID of the resource. The UID is assigned when the resource is created, and it is retained until it is deleted. */
	// +optional
	Uid *string `json:"uid,omitempty"`

	/* Time the Backup was updated in UTC. */
	// +optional
	UpdateTime *string `json:"updateTime,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AlloyDBBackup is the Schema for the alloydb API
// +k8s:openapi-gen=true
type AlloyDBBackup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AlloyDBBackupSpec   `json:"spec,omitempty"`
	Status AlloyDBBackupStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AlloyDBBackupList contains a list of AlloyDBBackup
type AlloyDBBackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AlloyDBBackup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AlloyDBBackup{}, &AlloyDBBackupList{})
}