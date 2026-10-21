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

package workstations

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/workstations/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/workstations/apiv1"
	pb "cloud.google.com/go/workstations/apiv1/workstationspb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.WorkstationConfigGVK, NewWorkstationConfigModel)
}

func NewWorkstationConfigModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelWorkstationConfig{config: *config}, nil
}

var _ directbase.Model = &modelWorkstationConfig{}
var _ = strings.TrimPrefix

type modelWorkstationConfig struct {
	config config.ControllerConfig
}

func (m *modelWorkstationConfig) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.WorkstationConfig{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.WorkstationConfigIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &WorkstationConfigAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelWorkstationConfig) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelWorkstationConfig) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building WorkstationConfig client: %w", err)
	}
	return gcpClient, err
}

type WorkstationConfigAdapter struct {
	id        *krm.WorkstationConfigIdentity
	gcpClient *gcp.Client
	desired   *krm.WorkstationConfig
	actual    *pb.WorkstationConfig
}

var _ directbase.Adapter = &WorkstationConfigAdapter{}

func (a *WorkstationConfigAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting WorkstationConfig", "name", a.id)

	req := &pb.GetWorkstationConfigRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetWorkstationConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting WorkstationConfig %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *WorkstationConfigAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating WorkstationConfig", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := WorkstationConfigSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateWorkstationConfigRequest{
		Parent:              a.id.ParentString(),
		WorkstationConfigId: a.id.WorkstationCluster,
		WorkstationConfig:   resource,
	}
	op, err := a.gcpClient.CreateWorkstationConfig(ctx, req)
	if err != nil {
		return fmt.Errorf("creating WorkstationConfig %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create WorkstationConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created WorkstationConfig", "name", a.id)

	status := &krm.WorkstationConfigStatus{}
	status.ObservedState = WorkstationConfigObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *WorkstationConfigAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating WorkstationConfig", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := WorkstationConfigSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareWorkstationConfig(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateWorkstationConfigRequest{
			WorkstationConfig: desired,
			UpdateMask:        updateMask,
		}
		op, err := a.gcpClient.UpdateWorkstationConfig(ctx, req)
		if err != nil {
			return fmt.Errorf("updating WorkstationConfig %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update WorkstationConfig %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated WorkstationConfig", "name", a.id)

		status := &krm.WorkstationConfigStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = WorkstationConfigObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareWorkstationConfig(ctx context.Context, actual, desired *pb.WorkstationConfig) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, WorkstationConfigSpec_FromProto, WorkstationConfigSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *WorkstationConfigAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.WorkstationConfig{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(WorkstationConfigSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.WorkstationCluster)
	u.SetGroupVersionKind(krm.WorkstationConfigGVK)
	return u, nil
}

func (a *WorkstationConfigAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting WorkstationConfig", "name", a.id)

	req := &pb.DeleteWorkstationConfigRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteWorkstationConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting WorkstationConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted WorkstationConfig", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete WorkstationConfig %s: %w", a.id, err)
		}
	}
	return true, nil
}
