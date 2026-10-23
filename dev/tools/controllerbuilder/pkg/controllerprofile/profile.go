// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controllerprofile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ControllerProfile is the unified specification for generating a Config Connector direct reconciler.
// It is composed of four orthogonal traits: Identity, Lifecycle, Execution, and Transport.
type ControllerProfile struct {
	// KRM Metadata
	Package       string // e.g., "vertexaifeatureonlinestore" or "parametermanager"
	KCCService    string // e.g., "aiplatform" or "cloudchannel"
	KCCVersion    string // e.g., "v1alpha1" or "v1beta1"
	Kind          string // e.g., "AIPlatformFeatureView"
	ProtoResource string // e.g., "FeatureView"
	ProtoVersion  string // e.g., "v1"

	// Constituent Traits
	Identity  IdentityTrait
	Lifecycle LifecycleTrait
	Execution ExecutionTrait
	Transport TransportTrait
}

// IdentityTrait defines how the resource identity and URI hierarchy are resolved.
type IdentityTrait struct {
	Strategy         IdentityStrategy
	TypeName         string // e.g., "krm.AIPlatformFeatureViewIdentity"
	UseGetIdentity   bool   // true if (*<Kind>) GetIdentity(ctx, reader) is available
	ConstructorName              string `json:"constructorName,omitempty" yaml:"constructorname,omitempty"`
	ConstructorTakesUnstructured bool   `json:"constructorTakesUnstructured,omitempty" yaml:"constructortakesunstructured,omitempty"`
	ParentStringExpr             string // e.g., "a.id.ParentString()" or "a.id.Parent().String()"
	IDExpr           string // e.g., "a.id.ID()" or "a.id.FeatureView"
	NameField        string // Resource proto name field, usually "Name"
	HasFromExternal      bool   // true if Identity supports FromExternal(string)
	HasIdentitySpecified bool   // true if Identity supports HasIdentitySpecified()
	HasProjectRef        bool   // true if resource has Spec.ProjectRef
	HasLocation          bool   // true if resource has Spec.Location
	HasResourceID        bool   // true if resource has Spec.ResourceID

	// Orthogonal facts matching PR #14224
	CreateHasIDField  bool   `json:"createHasIDField,omitempty" yaml:"createhasidfield,omitempty"`
	ServerGeneratedID bool   `json:"serverGeneratedID,omitempty" yaml:"servergeneratedid,omitempty"`
	UseListForFind    bool   `json:"useListForFind,omitempty" yaml:"uselistforfind,omitempty"`
	NameIsOutputOnly  bool   `json:"nameIsOutputOnly,omitempty" yaml:"nameisoutputonly,omitempty"`
	ProjectRefExpr    string `json:"projectRefExpr,omitempty" yaml:"projectrefexpr,omitempty"`
	LocationExpr      string `json:"locationExpr,omitempty" yaml:"locationexpr,omitempty"`
	ResourceIDExpr    string `json:"resourceIDExpr,omitempty" yaml:"resourceidexpr,omitempty"`
}

// HasClientBody returns true if client sets resource name inside resource body.
func (t IdentityTrait) HasClientBody(lifecycle LifecycleTrait) bool {
	if t.CreateHasIDField || lifecycle.CreateIDField != "" || t.Strategy == IdentityClientField {
		return false
	}
	if t.ServerGeneratedID || t.Strategy == IdentityServerGenerated || lifecycle.IsSingleton {
		return false
	}
	return t.Strategy == IdentityClientBody || (!t.ServerGeneratedID && t.NameField != "")
}

// IdentityStrategy specifies how the resource ID is passed on creation and tracked.
type IdentityStrategy string

const (
	// IdentityClientField indicates standard AIP-133: client supplies ID in a designated request field (e.g. req.<resource>_id).
	IdentityClientField IdentityStrategy = "client_field"
	// IdentityClientBody indicates the client sets the resource name inside the resource body (e.g. req.Resource.Name = a.id.String()).
	IdentityClientBody IdentityStrategy = "client_body"
	// IdentityServerGenerated indicates the server generates the ID. In Find(), if !HasIdentitySpecified(), return false.
	IdentityServerGenerated IdentityStrategy = "server_generated"
	// IdentitySingleton indicates an AIP-156 singleton with a static path and no child ID parameter.
	IdentitySingleton IdentityStrategy = "singleton"
)

// LifecycleTrait defines the CRUD lifecycle capabilities and mutation semantics.
type LifecycleTrait struct {
	HasGet      bool
	CanCreate   bool
	UpdateStyle UpdateStyle
	DeleteStyle DeleteStyle
	IsSingleton bool

	// Orthogonal facts matching PR #14224
	HasUpdateFieldMask bool `json:"hasUpdateFieldMask,omitempty" yaml:"hasupdatefieldmask,omitempty"`
	NoUpdate           bool `json:"noUpdate,omitempty" yaml:"noupdate,omitempty"`
	NoDelete           bool `json:"noDelete,omitempty" yaml:"nodelete,omitempty"`

	// Method & Request Names
	GetMethodName  string
	GetRequestType string
	GetNameField   string

	CreateMethodName  string
	CreateRequestType string
	CreateParentField string
	CreateBodyField   string
	CreateIDField     string

	UpdateMethodName      string
	UpdateRequestType     string
	UpdateBodyField       string
	UpdateMaskField       string
	UpdateHasTopLevelName bool

	DeleteMethodName  string
	DeleteRequestType string
	DeleteNameField   string
	DeleteForceField  string
}

// UpdateStyle specifies how updates are applied to the GCP resource.
type UpdateStyle string

const (
	// UpdateStyleMask indicates AIP-134 field mask updates via CompareGreenfieldSpec.
	UpdateStyleMask UpdateStyle = "mask"
	// UpdateStyleFullReplacement indicates the API takes a full replacement proto without update_mask.
	UpdateStyleFullReplacement UpdateStyle = "full_replacement"
	// UpdateStyleUnsupported indicates updates are not permitted; spec modifications trigger an error.
	UpdateStyleUnsupported UpdateStyle = "unsupported"
)

// DeleteStyle specifies how deletions are handled.
type DeleteStyle string

const (
	// DeleteStyleStandard indicates a remote Delete RPC is invoked.
	DeleteStyleStandard DeleteStyle = "standard"
	// DeleteStyleUnsupported indicates the resource cannot be deleted; finalizer is removed without remote call.
	DeleteStyleUnsupported DeleteStyle = "unsupported"
)

// ExecutionTrait specifies synchronous vs. asynchronous execution and response shapes.
type ExecutionTrait struct {
	Create OpExecution
	Update OpExecution
	Delete OpExecution
}

// OpExecution specifies execution semantics for a single RPC method.
type OpExecution struct {
	IsLRO   bool // true if returns google.longrunning.Operation
	IsEmpty bool // true if response is google.protobuf.Empty
}

// TransportTrait specifies GAPIC client wiring, endpoint conventions, and schema bindings.
type TransportTrait struct {
	// Client Wiring
	GCPClientImportPath  string // e.g., "cloud.google.com/go/channel/apiv1"
	GCPClientType        string // e.g., "*gcp.CloudChannelClient"
	GCPClientConstructor string // e.g., "gcp.NewCloudChannelRESTClient"
	UseGRPCClient        bool   // true if uses GRPCClientOptions() instead of REST

	// Protobuf Wiring
	PBImportPath   string // e.g., "cloud.google.com/go/channel/apiv1/channelpb"
	PBPackageAlias string // e.g., "channelpb"

	// Routing & Endpoints
	RegionalEndpoint string // "" | "prefix" | "rep"
	ServiceHost      string // e.g., "cloudkms.googleapis.com"
	LocationExpr     string // e.g., "direct.ValueOf(obj.Spec.Location)"

	// Schema & Reference Traits
	LabelDiffing        LabelDiffingStyle
	HasLabels           bool   // true if resource supports labels
	HasReferences       bool   // Spec has *Ref fields -> emits common.NormalizeReferences
	HasObservedState    bool   // Status has ObservedState field
	MapperVersionSuffix string // e.g., "" or "_v1alpha1"
}

// LabelDiffingStyle specifies how labels are managed.
type LabelDiffingStyle string

const (
	// LabelsNone indicates the resource does not have labels.
	LabelsNone LabelDiffingStyle = "none"
	// LabelsMetadata indicates labels are compared against metadata.labels using CompareGreenfieldSpecAndLabels.
	LabelsMetadata LabelDiffingStyle = "metadata"
	// LabelsSpec indicates labels are declared in Spec.Labels and compared as part of Spec.
	LabelsSpec LabelDiffingStyle = "spec"
)

// Helper methods on ControllerProfile for template rendering

func (p *ControllerProfile) NeedsCreatedVar() bool {
	if !p.Lifecycle.CanCreate {
		return false
	}
	if p.Execution.Create.IsEmpty {
		return false
	}
	// created is only referenced if we map ObservedState or if ID is server_generated
	return p.Transport.HasObservedState || p.Identity.Strategy == IdentityServerGenerated || p.Identity.ServerGeneratedID
}

func (p *ControllerProfile) NeedsUpdatedVar() bool {
	if p.Lifecycle.NoUpdate || p.Lifecycle.UpdateStyle == UpdateStyleUnsupported {
		return false
	}
	if p.Execution.Update.IsEmpty {
		return false
	}
	// updated is only referenced if we map ObservedState
	return p.Transport.HasObservedState
}

func (p *ControllerProfile) UpdateCompareLHS() string {
	if p.Lifecycle.UpdateStyle == UpdateStyleUnsupported {
		return "_, report, updateMask, err"
	}
	resVar := "_"
	if p.Lifecycle.UpdateBodyField != "" || p.Identity.NameField != "" {
		resVar = "resource"
	}
	maskVar := "_"
	if p.Lifecycle.UpdateStyle == UpdateStyleMask {
		maskVar = "updateMask"
	}
	return fmt.Sprintf("%s, report, %s, err", resVar, maskVar)
}

func (p *ControllerProfile) UsesRefsPackage() bool {
	if p.Identity.ProjectRefExpr != "" {
		return strings.Contains(p.Identity.ProjectRefExpr, "refs.")
	}
	return p.Identity.HasProjectRef
}

func (p *ControllerProfile) UsesParentPackage() bool {
	return strings.Contains(p.Identity.ProjectRefExpr, "parent.") || strings.Contains(p.Identity.LocationExpr, "parent.")
}

func (p *ControllerProfile) ProtoResourceLower() string {
	return strings.ToLower(p.ProtoResource)
}

// LoadFromYAML loads a ControllerProfile from a YAML file.
func LoadFromYAML(filePath string) (*ControllerProfile, error) {
	b, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading profile %s: %w", filePath, err)
	}
	var p ControllerProfile
	if err := yaml.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("unmarshaling profile %s: %w", filePath, err)
	}
	return &p, nil
}

// SaveToYAML serializes the ControllerProfile to a YAML file.
func (p *ControllerProfile) SaveToYAML(filePath string) error {
	b, err := yaml.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshaling profile to YAML: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("creating directory for profile: %w", err)
	}
	return os.WriteFile(filePath, b, 0644)
}
