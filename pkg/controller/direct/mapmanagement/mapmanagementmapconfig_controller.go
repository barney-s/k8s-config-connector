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

package mapmanagement

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/mapmanagement/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/maps/mapmanagement/apiv2beta"
	pb "cloud.google.com/go/maps/mapmanagement/apiv2beta/mapmanagementpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.MapManagementMapConfigGVK, NewMapConfigModel)
}

func NewMapConfigModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelMapConfig{config: *config}, nil
}

var _ directbase.Model = &modelMapConfig{}
var _ = strings.TrimPrefix

type modelMapConfig struct {
	config config.ControllerConfig
}

func (m *modelMapConfig) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.MapManagementMapConfig{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.MapManagementMapConfigIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &MapConfigAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelMapConfig) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.MapManagementMapConfigIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &MapConfigAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelMapConfig) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building MapConfig client: %w", err)
	}
	return gcpClient, err
}

type MapConfigAdapter struct {
	id        *krm.MapManagementMapConfigIdentity
	gcpClient *gcp.Client
	desired   *krm.MapManagementMapConfig
	actual    *pb.MapConfig
}

var _ directbase.Adapter = &MapConfigAdapter{}

func (a *MapConfigAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting MapConfig", "name", a.id)

	req := &pb.GetMapConfigRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetMapConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting MapConfig %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *MapConfigAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating MapConfig", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := MapManagementMapConfigSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateMapConfigRequest{
		Parent:      a.id.ParentString(),
		MapConfigId: a.id.MapConfig,
		MapConfig:   resource,
	}
	created, err := a.gcpClient.CreateMapConfig(ctx, req)
	if err != nil {
		return fmt.Errorf("creating MapConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created MapConfig", "name", a.id)

	status := &krm.MapManagementMapConfigStatus{}
	status.ObservedState = MapManagementMapConfigObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *MapConfigAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating MapConfig", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := MapManagementMapConfigSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareMapConfig(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateMapConfigRequest{
			MapConfig:  desired,
			UpdateMask: updateMask,
		}
		updated, err := a.gcpClient.UpdateMapConfig(ctx, req)
		if err != nil {
			return fmt.Errorf("updating MapConfig %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated MapConfig", "name", a.id)

		status := &krm.MapManagementMapConfigStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = MapManagementMapConfigObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareMapConfig(ctx context.Context, actual, desired *pb.MapConfig) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, MapManagementMapConfigSpec_FromProto, MapManagementMapConfigSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *MapConfigAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.MapManagementMapConfig{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(MapManagementMapConfigSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.ResourceID = direct.LazyPtr(a.id.MapConfig)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.MapConfig)
	u.SetGroupVersionKind(krm.MapManagementMapConfigGVK)
	return u, nil
}

func (a *MapConfigAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting MapConfig", "name", a.id)

	req := &pb.DeleteMapConfigRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteMapConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting MapConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted MapConfig", "name", a.id)
	return true, nil
}
