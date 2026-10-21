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

package iap

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/iap/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/iap/apiv1"
	pb "cloud.google.com/go/iap/apiv1/iappb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.IAPSettingsGVK, NewIapSettingsModel)
}

func NewIapSettingsModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelIapSettings{config: *config}, nil
}

var _ directbase.Model = &modelIapSettings{}
var _ = strings.TrimPrefix

type modelIapSettings struct {
	config config.ControllerConfig
}

func (m *modelIapSettings) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.IAPSettings{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.IAPSettingsIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &IapSettingsAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelIapSettings) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelIapSettings) client(ctx context.Context) (*gcp.IdentityAwareProxyAdminClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewIdentityAwareProxyAdminRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building IapSettings client: %w", err)
	}
	return gcpClient, err
}

type IapSettingsAdapter struct {
	id        *krm.IAPSettingsIdentity
	gcpClient *gcp.IdentityAwareProxyAdminClient
	desired   *krm.IAPSettings
	actual    *pb.IapSettings
}

var _ directbase.Adapter = &IapSettingsAdapter{}

func (a *IapSettingsAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting IapSettings", "name", a.id)

	req := &pb.GetIapSettingsRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetIapSettings(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting IapSettings %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *IapSettingsAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating IapSettings", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := IAPSettingsSpec_v1beta1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateIapSettingsRequest{
		Parent:        a.id.ParentString(),
		IapSettingsId: a.id.IapSettings,
		IapSettings:   resource,
	}
	created, err := a.gcpClient.CreateIapSettings(ctx, req)
	if err != nil {
		return fmt.Errorf("creating IapSettings %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created IapSettings", "name", a.id)

	status := &krm.IAPSettingsStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *IapSettingsAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating IapSettings", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := IAPSettingsSpec_v1beta1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareIapSettings(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateIapSettingsRequest{
			IapSettings: desired,
			UpdateMask:  updateMask,
		}
		updated, err := a.gcpClient.UpdateIapSettings(ctx, req)
		if err != nil {
			return fmt.Errorf("updating IapSettings %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated IapSettings", "name", a.id)

		status := &krm.IAPSettingsStatus{}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareIapSettings(ctx context.Context, actual, desired *pb.IapSettings) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, IAPSettingsSpec_v1beta1_FromProto, IAPSettingsSpec_v1beta1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *IapSettingsAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.IAPSettings{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(IAPSettingsSpec_v1beta1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.IapSettings)
	u.SetGroupVersionKind(krm.IAPSettingsGVK)
	return u, nil
}

func (a *IapSettingsAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deletion not supported for IapSettings, removing finalizer without deleting GCP resource", "name", a.id)
	return true, nil
}
