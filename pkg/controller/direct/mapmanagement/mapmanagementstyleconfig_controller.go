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
	registry.RegisterModel(krm.MapManagementStyleConfigGVK, NewStyleConfigModel)
}

func NewStyleConfigModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelStyleConfig{config: *config}, nil
}

var _ directbase.Model = &modelStyleConfig{}
var _ = strings.TrimPrefix

type modelStyleConfig struct {
	config config.ControllerConfig
}

func (m *modelStyleConfig) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.MapManagementStyleConfig{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.MapManagementStyleConfigIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &StyleConfigAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelStyleConfig) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.MapManagementStyleConfigIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &StyleConfigAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelStyleConfig) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building StyleConfig client: %w", err)
	}
	return gcpClient, err
}

type StyleConfigAdapter struct {
	id        *krm.MapManagementStyleConfigIdentity
	gcpClient *gcp.Client
	desired   *krm.MapManagementStyleConfig
	actual    *pb.StyleConfig
}

var _ directbase.Adapter = &StyleConfigAdapter{}

func (a *StyleConfigAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting StyleConfig", "name", a.id)

	req := &pb.GetStyleConfigRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetStyleConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting StyleConfig %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *StyleConfigAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating StyleConfig", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := MapManagementStyleConfigSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateStyleConfigRequest{
		Parent:        a.id.ParentString(),
		StyleConfigId: a.id.StyleConfig,
		StyleConfig:   resource,
	}
	created, err := a.gcpClient.CreateStyleConfig(ctx, req)
	if err != nil {
		return fmt.Errorf("creating StyleConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created StyleConfig", "name", a.id)

	status := &krm.MapManagementStyleConfigStatus{}
	status.ObservedState = MapManagementStyleConfigObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *StyleConfigAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating StyleConfig", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := MapManagementStyleConfigSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareStyleConfig(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateStyleConfigRequest{
			StyleConfig: desired,
			UpdateMask:  updateMask,
		}
		updated, err := a.gcpClient.UpdateStyleConfig(ctx, req)
		if err != nil {
			return fmt.Errorf("updating StyleConfig %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated StyleConfig", "name", a.id)

		status := &krm.MapManagementStyleConfigStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = MapManagementStyleConfigObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareStyleConfig(ctx context.Context, actual, desired *pb.StyleConfig) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, MapManagementStyleConfigSpec_FromProto, MapManagementStyleConfigSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *StyleConfigAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.MapManagementStyleConfig{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(MapManagementStyleConfigSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.ResourceID = direct.LazyPtr(a.id.StyleConfig)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.StyleConfig)
	u.SetGroupVersionKind(krm.MapManagementStyleConfigGVK)
	return u, nil
}

func (a *StyleConfigAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting StyleConfig", "name", a.id)

	req := &pb.DeleteStyleConfigRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteStyleConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting StyleConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted StyleConfig", "name", a.id)
	return true, nil
}
