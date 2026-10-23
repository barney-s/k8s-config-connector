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

package gkebackup

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/gkebackup/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/export"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/label"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/gkebackup/apiv1"
	pb "cloud.google.com/go/gkebackup/apiv1/gkebackuppb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.GKEBackupRestoreGVK, NewRestoreModel)
}

func NewRestoreModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelRestore{config: *config}, nil
}

var _ directbase.Model = &modelRestore{}
var _ = strings.TrimPrefix

type modelRestore struct {
	config config.ControllerConfig
}

func (m *modelRestore) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.GKEBackupRestore{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id, err := krm.NewRestoreIdentity(ctx, reader, obj)
	if err != nil {
		return nil, err
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &RestoreAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelRestore) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelRestore) client(ctx context.Context) (*gcp.BackupForGKEClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewBackupForGKERESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Restore client: %w", err)
	}
	return gcpClient, err
}

type RestoreAdapter struct {
	id        *krm.RestoreIdentity
	gcpClient *gcp.BackupForGKEClient
	desired   *krm.GKEBackupRestore
	actual    *pb.Restore
}

var _ directbase.Adapter = &RestoreAdapter{}

func (a *RestoreAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Restore", "name", a.id)

	req := &pb.GetRestoreRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetRestore(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Restore %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *RestoreAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Restore", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := GKEBackupRestoreSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateRestoreRequest{
		Parent:    a.id.Parent().String(),
		RestoreId: a.id.ID(),
		Restore:   resource,
	}
	op, err := a.gcpClient.CreateRestore(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Restore %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create Restore %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Restore", "name", a.id)

	status := &krm.GKEBackupRestoreStatus{}
	status.ObservedState = GKEBackupRestoreObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *RestoreAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Restore", "name", a.id)

	desired, diffs, updateMask, err := compareRestore(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateRestoreRequest{
			Restore:    desired,
			UpdateMask: updateMask,
		}
		op, err := a.gcpClient.UpdateRestore(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Restore %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update Restore %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Restore", "name", a.id)

		status := &krm.GKEBackupRestoreStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = GKEBackupRestoreObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareRestore(ctx context.Context, u *unstructured.Unstructured, actual *pb.Restore, desiredSpec *krm.GKEBackupRestoreSpec) (*pb.Restore, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	return common.CompareGreenfieldSpecAndLabels(
		ctx,
		u,
		desiredSpec,
		actual,
		"labels",
		GKEBackupRestoreSpec_FromProto,
		GKEBackupRestoreSpec_ToProto,
		nil,
	)
}

func (a *RestoreAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.GKEBackupRestore{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(GKEBackupRestoreSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.GKEBackupRestoreGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *RestoreAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Restore", "name", a.id)

	req := &pb.DeleteRestoreRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteRestore(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Restore %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Restore", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete Restore %s: %w", a.id, err)
		}
	}
	return true, nil
}
