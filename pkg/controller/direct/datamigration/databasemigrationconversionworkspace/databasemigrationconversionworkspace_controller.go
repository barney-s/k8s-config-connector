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

package databasemigrationconversionworkspace

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/datamigration/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/clouddms/apiv1"
	pb "cloud.google.com/go/clouddms/apiv1/clouddmspb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.DatabaseMigrationConversionWorkspaceGVK, NewConversionWorkspaceModel)
}

func NewConversionWorkspaceModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelConversionWorkspace{config: *config}, nil
}

var _ directbase.Model = &modelConversionWorkspace{}
var _ = strings.TrimPrefix

type modelConversionWorkspace struct {
	config config.ControllerConfig
}

func (m *modelConversionWorkspace) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.DatabaseMigrationConversionWorkspace{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.DatabaseMigrationConversionWorkspaceIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ConversionWorkspaceAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelConversionWorkspace) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelConversionWorkspace) client(ctx context.Context) (*gcp.DataMigrationClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.GRPCClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewDataMigrationClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ConversionWorkspace client: %w", err)
	}
	return gcpClient, err
}

type ConversionWorkspaceAdapter struct {
	id        *krm.DatabaseMigrationConversionWorkspaceIdentity
	gcpClient *gcp.DataMigrationClient
	desired   *krm.DatabaseMigrationConversionWorkspace
	actual    *pb.ConversionWorkspace
}

var _ directbase.Adapter = &ConversionWorkspaceAdapter{}

func (a *ConversionWorkspaceAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ConversionWorkspace", "name", a.id)

	req := &pb.GetConversionWorkspaceRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetConversionWorkspace(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ConversionWorkspace %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ConversionWorkspaceAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ConversionWorkspace", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := DatabaseMigrationConversionWorkspaceSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateConversionWorkspaceRequest{
		Parent:                a.id.ParentString(),
		ConversionWorkspaceId: a.id.ConversionWorkspace,
		ConversionWorkspace:   resource,
	}
	op, err := a.gcpClient.CreateConversionWorkspace(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ConversionWorkspace %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create ConversionWorkspace %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ConversionWorkspace", "name", a.id)

	status := &krm.DatabaseMigrationConversionWorkspaceStatus{}
	status.ObservedState = DatabaseMigrationConversionWorkspaceObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ConversionWorkspaceAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ConversionWorkspace", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := DatabaseMigrationConversionWorkspaceSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareConversionWorkspace(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateConversionWorkspaceRequest{
			ConversionWorkspace: desired,
			UpdateMask:          updateMask,
		}
		op, err := a.gcpClient.UpdateConversionWorkspace(ctx, req)
		if err != nil {
			return fmt.Errorf("updating ConversionWorkspace %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update ConversionWorkspace %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated ConversionWorkspace", "name", a.id)

		status := &krm.DatabaseMigrationConversionWorkspaceStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = DatabaseMigrationConversionWorkspaceObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareConversionWorkspace(ctx context.Context, actual, desired *pb.ConversionWorkspace) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, DatabaseMigrationConversionWorkspaceSpec_FromProto, DatabaseMigrationConversionWorkspaceSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *ConversionWorkspaceAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.DatabaseMigrationConversionWorkspace{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(DatabaseMigrationConversionWorkspaceSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ConversionWorkspace)
	u.SetGroupVersionKind(krm.DatabaseMigrationConversionWorkspaceGVK)
	return u, nil
}

func (a *ConversionWorkspaceAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting ConversionWorkspace", "name", a.id)

	req := &pb.DeleteConversionWorkspaceRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteConversionWorkspace(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting ConversionWorkspace %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted ConversionWorkspace", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete ConversionWorkspace %s: %w", a.id, err)
		}
	}
	return true, nil
}
