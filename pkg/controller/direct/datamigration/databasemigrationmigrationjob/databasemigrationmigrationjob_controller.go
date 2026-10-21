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

package databasemigrationmigrationjob

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
	registry.RegisterModel(krm.DatabaseMigrationMigrationJobGVK, NewMigrationJobModel)
}

func NewMigrationJobModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelMigrationJob{config: *config}, nil
}

var _ directbase.Model = &modelMigrationJob{}
var _ = strings.TrimPrefix

type modelMigrationJob struct {
	config config.ControllerConfig
}

func (m *modelMigrationJob) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.DatabaseMigrationMigrationJob{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.DatabaseMigrationMigrationJobIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &MigrationJobAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelMigrationJob) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelMigrationJob) client(ctx context.Context) (*gcp.DataMigrationClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.GRPCClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewDataMigrationClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building MigrationJob client: %w", err)
	}
	return gcpClient, err
}

type MigrationJobAdapter struct {
	id        *krm.DatabaseMigrationMigrationJobIdentity
	gcpClient *gcp.DataMigrationClient
	desired   *krm.DatabaseMigrationMigrationJob
	actual    *pb.MigrationJob
}

var _ directbase.Adapter = &MigrationJobAdapter{}

func (a *MigrationJobAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting MigrationJob", "name", a.id)

	req := &pb.GetMigrationJobRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetMigrationJob(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting MigrationJob %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *MigrationJobAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating MigrationJob", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := DatabaseMigrationMigrationJobSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateMigrationJobRequest{
		Parent:         a.id.ParentString(),
		MigrationJobId: a.id.MigrationJob,
		MigrationJob:   resource,
	}
	op, err := a.gcpClient.CreateMigrationJob(ctx, req)
	if err != nil {
		return fmt.Errorf("creating MigrationJob %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create MigrationJob %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created MigrationJob", "name", a.id)

	status := &krm.DatabaseMigrationMigrationJobStatus{}
	status.ObservedState = DatabaseMigrationMigrationJobObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *MigrationJobAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating MigrationJob", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := DatabaseMigrationMigrationJobSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareMigrationJob(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateMigrationJobRequest{
			MigrationJob: desired,
			UpdateMask:   updateMask,
		}
		op, err := a.gcpClient.UpdateMigrationJob(ctx, req)
		if err != nil {
			return fmt.Errorf("updating MigrationJob %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update MigrationJob %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated MigrationJob", "name", a.id)

		status := &krm.DatabaseMigrationMigrationJobStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = DatabaseMigrationMigrationJobObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareMigrationJob(ctx context.Context, actual, desired *pb.MigrationJob) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, DatabaseMigrationMigrationJobSpec_FromProto, DatabaseMigrationMigrationJobSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *MigrationJobAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.DatabaseMigrationMigrationJob{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(DatabaseMigrationMigrationJobSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.MigrationJob)
	u.SetGroupVersionKind(krm.DatabaseMigrationMigrationJobGVK)
	return u, nil
}

func (a *MigrationJobAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting MigrationJob", "name", a.id)

	req := &pb.DeleteMigrationJobRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteMigrationJob(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting MigrationJob %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted MigrationJob", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete MigrationJob %s: %w", a.id, err)
		}
	}
	return true, nil
}
