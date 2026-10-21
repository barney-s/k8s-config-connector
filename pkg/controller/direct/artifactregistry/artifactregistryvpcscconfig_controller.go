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

package artifactregistry

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/artifactregistry/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/artifactregistry/apiv1"
	pb "cloud.google.com/go/artifactregistry/apiv1/artifactregistrypb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.ArtifactRegistryVPCSCConfigGVK, NewVPCSCConfigModel)
}

func NewVPCSCConfigModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelVPCSCConfig{config: *config}, nil
}

var _ directbase.Model = &modelVPCSCConfig{}
var _ = strings.TrimPrefix

type modelVPCSCConfig struct {
	config config.ControllerConfig
}

func (m *modelVPCSCConfig) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.ArtifactRegistryVPCSCConfig{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.ArtifactRegistryVPCSCConfigIdentity)

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &VPCSCConfigAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelVPCSCConfig) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelVPCSCConfig) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building VPCSCConfig client: %w", err)
	}
	return gcpClient, err
}

type VPCSCConfigAdapter struct {
	id        *krm.ArtifactRegistryVPCSCConfigIdentity
	gcpClient *gcp.Client
	desired   *krm.ArtifactRegistryVPCSCConfig
	actual    *pb.VPCSCConfig
}

var _ directbase.Adapter = &VPCSCConfigAdapter{}

func (a *VPCSCConfigAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting VPCSCConfig", "name", a.id)

	req := &pb.GetVPCSCConfigRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetVPCSCConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting VPCSCConfig %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *VPCSCConfigAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating VPCSCConfig", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := ArtifactRegistryVPCSCConfigSpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateVPCSCConfigRequest{
		Parent:        a.id.ParentString(),
		VPCSCConfigId: a.id.VPCSCConfig,
		VPCSCConfig:   resource,
	}
	created, err := a.gcpClient.CreateVPCSCConfig(ctx, req)
	if err != nil {
		return fmt.Errorf("creating VPCSCConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created VPCSCConfig", "name", a.id)

	status := &krm.ArtifactRegistryVPCSCConfigStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *VPCSCConfigAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating VPCSCConfig", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := ArtifactRegistryVPCSCConfigSpec_v1alpha1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareVPCSCConfig(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateVPCSCConfigRequest{
			VPCSCConfig: desired,
			UpdateMask:  updateMask,
		}
		updated, err := a.gcpClient.UpdateVPCSCConfig(ctx, req)
		if err != nil {
			return fmt.Errorf("updating VPCSCConfig %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated VPCSCConfig", "name", a.id)

		status := &krm.ArtifactRegistryVPCSCConfigStatus{}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareVPCSCConfig(ctx context.Context, actual, desired *pb.VPCSCConfig) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, ArtifactRegistryVPCSCConfigSpec_v1alpha1_FromProto, ArtifactRegistryVPCSCConfigSpec_v1alpha1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *VPCSCConfigAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.ArtifactRegistryVPCSCConfig{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(ArtifactRegistryVPCSCConfigSpec_v1alpha1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.Location = a.id.Location

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.VPCSCConfig)
	u.SetGroupVersionKind(krm.ArtifactRegistryVPCSCConfigGVK)
	return u, nil
}

func (a *VPCSCConfigAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deletion not supported for VPCSCConfig, removing finalizer without deleting GCP resource", "name", a.id)
	return true, nil
}
