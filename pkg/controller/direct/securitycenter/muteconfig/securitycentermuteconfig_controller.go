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

package muteconfig

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/securitycenter/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/securitycenter/apiv1"
	securitycenterpb "cloud.google.com/go/securitycenter/apiv1/securitycenterpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.SecurityCenterMuteConfigGVK, NewMuteConfigModel)
}

func NewMuteConfigModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelMuteConfig{config: *config}, nil
}

var _ directbase.Model = &modelMuteConfig{}
var _ = strings.TrimPrefix

type modelMuteConfig struct {
	config config.ControllerConfig
}

func (m *modelMuteConfig) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.SecurityCenterMuteConfig{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.SecurityCenterMuteConfigIdentity)

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &MuteConfigAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelMuteConfig) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.SecurityCenterMuteConfigIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &MuteConfigAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelMuteConfig) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building MuteConfig client: %w", err)
	}
	return gcpClient, err
}

type MuteConfigAdapter struct {
	id        *krm.SecurityCenterMuteConfigIdentity
	gcpClient *gcp.Client
	desired   *krm.SecurityCenterMuteConfig
	actual    *securitycenterpb.MuteConfig
}

var _ directbase.Adapter = &MuteConfigAdapter{}

func (a *MuteConfigAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting MuteConfig", "name", a.id)

	req := &securitycenterpb.GetMuteConfigRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetMuteConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting MuteConfig %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *MuteConfigAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating MuteConfig", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := SecurityCenterMuteConfigSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &securitycenterpb.CreateMuteConfigRequest{
		Parent:       a.id.ParentString(),
		MuteConfigId: a.id.Organization,
		MuteConfig:   resource,
	}
	created, err := a.gcpClient.CreateMuteConfig(ctx, req)
	if err != nil {
		return fmt.Errorf("creating MuteConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created MuteConfig", "name", a.id)

	status := &krm.SecurityCenterMuteConfigStatus{}
	status.ObservedState = SecurityCenterMuteConfigObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *MuteConfigAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating MuteConfig", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := SecurityCenterMuteConfigSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareMuteConfig(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &securitycenterpb.UpdateMuteConfigRequest{
			MuteConfig: desired,
			UpdateMask: updateMask,
		}
		updated, err := a.gcpClient.UpdateMuteConfig(ctx, req)
		if err != nil {
			return fmt.Errorf("updating MuteConfig %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated MuteConfig", "name", a.id)

		status := &krm.SecurityCenterMuteConfigStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = SecurityCenterMuteConfigObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareMuteConfig(ctx context.Context, actual, desired *securitycenterpb.MuteConfig) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, SecurityCenterMuteConfigSpec_FromProto, SecurityCenterMuteConfigSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *MuteConfigAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.SecurityCenterMuteConfig{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(SecurityCenterMuteConfigSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Organization)
	u.SetGroupVersionKind(krm.SecurityCenterMuteConfigGVK)
	return u, nil
}

func (a *MuteConfigAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting MuteConfig", "name", a.id)

	req := &securitycenterpb.DeleteMuteConfigRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteMuteConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting MuteConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted MuteConfig", "name", a.id)
	return true, nil
}
