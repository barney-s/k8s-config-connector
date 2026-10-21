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

package modelarmor

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/modelarmor/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/modelarmor/apiv1"
	pb "cloud.google.com/go/modelarmor/apiv1/modelarmorpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.ModelArmorFloorSettingGVK, NewFloorSettingModel)
}

func NewFloorSettingModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelFloorSetting{config: *config}, nil
}

var _ directbase.Model = &modelFloorSetting{}
var _ = strings.TrimPrefix

type modelFloorSetting struct {
	config config.ControllerConfig
}

func (m *modelFloorSetting) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.ModelArmorFloorSetting{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.ModelArmorFloorSettingIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &FloorSettingAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelFloorSetting) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelFloorSetting) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building FloorSetting client: %w", err)
	}
	return gcpClient, err
}

type FloorSettingAdapter struct {
	id        *krm.ModelArmorFloorSettingIdentity
	gcpClient *gcp.Client
	desired   *krm.ModelArmorFloorSetting
	actual    *pb.FloorSetting
}

var _ directbase.Adapter = &FloorSettingAdapter{}

func (a *FloorSettingAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting FloorSetting", "name", a.id)

	req := &pb.GetFloorSettingRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetFloorSetting(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting FloorSetting %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *FloorSettingAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating FloorSetting", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := ModelArmorFloorSettingSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateFloorSettingRequest{
		Parent:         a.id.ParentString(),
		FloorSettingId: a.id.FloorSetting,
		FloorSetting:   resource,
	}
	created, err := a.gcpClient.CreateFloorSetting(ctx, req)
	if err != nil {
		return fmt.Errorf("creating FloorSetting %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created FloorSetting", "name", a.id)

	status := &krm.ModelArmorFloorSettingStatus{}
	status.ObservedState = ModelArmorFloorSettingObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *FloorSettingAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating FloorSetting", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := ModelArmorFloorSettingSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareFloorSetting(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateFloorSettingRequest{
			FloorSetting: desired,
			UpdateMask:   updateMask,
		}
		updated, err := a.gcpClient.UpdateFloorSetting(ctx, req)
		if err != nil {
			return fmt.Errorf("updating FloorSetting %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated FloorSetting", "name", a.id)

		status := &krm.ModelArmorFloorSettingStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = ModelArmorFloorSettingObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareFloorSetting(ctx context.Context, actual, desired *pb.FloorSetting) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, ModelArmorFloorSettingSpec_FromProto, ModelArmorFloorSettingSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *FloorSettingAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.ModelArmorFloorSetting{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(ModelArmorFloorSettingSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.Location = a.id.Location

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.FloorSetting)
	u.SetGroupVersionKind(krm.ModelArmorFloorSettingGVK)
	return u, nil
}

func (a *FloorSettingAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deletion not supported for FloorSetting, removing finalizer without deleting GCP resource", "name", a.id)
	return true, nil
}
