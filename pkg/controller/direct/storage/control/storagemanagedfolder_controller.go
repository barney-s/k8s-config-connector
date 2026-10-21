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

package storagecontrol

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/storage/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/storage/control/apiv2"
	pb "cloud.google.com/go/storage/control/apiv2/controlpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.StorageManagedFolderGVK, NewManagedFolderModel)
}

func NewManagedFolderModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelManagedFolder{config: *config}, nil
}

var _ directbase.Model = &modelManagedFolder{}
var _ = strings.TrimPrefix

type modelManagedFolder struct {
	config config.ControllerConfig
}

func (m *modelManagedFolder) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.StorageManagedFolder{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.ManagedFolderIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ManagedFolderAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelManagedFolder) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelManagedFolder) client(ctx context.Context) (*gcp.StorageControlClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.GRPCClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewStorageControlClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ManagedFolder client: %w", err)
	}
	return gcpClient, err
}

type ManagedFolderAdapter struct {
	id        *krm.ManagedFolderIdentity
	gcpClient *gcp.StorageControlClient
	desired   *krm.StorageManagedFolder
	actual    *pb.ManagedFolder
}

var _ directbase.Adapter = &ManagedFolderAdapter{}

func (a *ManagedFolderAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ManagedFolder", "name", a.id)

	req := &pb.GetManagedFolderRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetManagedFolder(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ManagedFolder %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ManagedFolderAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ManagedFolder", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := StorageManagedFolderSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateManagedFolderRequest{
		Parent:          a.id.ParentString(),
		ManagedFolderId: a.id.ID(),
		ManagedFolder:   resource,
	}
	created, err := a.gcpClient.CreateManagedFolder(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ManagedFolder %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ManagedFolder", "name", a.id)

	status := &krm.StorageManagedFolderStatus{}
	status.ObservedState = StorageManagedFolderObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ManagedFolderAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ManagedFolder", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := StorageManagedFolderSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareManagedFolder(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("StorageManagedFolder resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.StorageManagedFolderStatus{}
	mapCtx = &direct.MapContext{}
	status.ObservedState = StorageManagedFolderObservedState_FromProto(mapCtx, a.actual)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareManagedFolder(ctx context.Context, actual, desired *pb.ManagedFolder) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, StorageManagedFolderSpec_FromProto, StorageManagedFolderSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *ManagedFolderAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.StorageManagedFolder{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(StorageManagedFolderSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.StorageManagedFolderGVK)
	return u, nil
}

func (a *ManagedFolderAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting ManagedFolder", "name", a.id)

	req := &pb.DeleteManagedFolderRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteManagedFolder(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting ManagedFolder %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted ManagedFolder", "name", a.id)
	return true, nil
}
