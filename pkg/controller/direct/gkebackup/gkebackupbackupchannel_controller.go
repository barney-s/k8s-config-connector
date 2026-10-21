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
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/export"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/label"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
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
	registry.RegisterModel(krm.GKEBackupBackupChannelGVK, NewBackupChannelModel)
}

func NewBackupChannelModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelBackupChannel{config: *config}, nil
}

var _ directbase.Model = &modelBackupChannel{}
var _ = strings.TrimPrefix

type modelBackupChannel struct {
	config config.ControllerConfig
}

func (m *modelBackupChannel) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.GKEBackupBackupChannel{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.BackupChannelIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &BackupChannelAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelBackupChannel) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelBackupChannel) client(ctx context.Context) (*gcp.BackupForGKEClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building BackupChannel client: %w", err)
	}
	return gcpClient, err
}

type BackupChannelAdapter struct {
	id        *krm.BackupChannelIdentity
	gcpClient *gcp.BackupForGKEClient
	desired   *krm.GKEBackupBackupChannel
	actual    *pb.BackupChannel
}

var _ directbase.Adapter = &BackupChannelAdapter{}

func (a *BackupChannelAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting BackupChannel", "name", a.id)

	req := &pb.GetBackupChannelRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetBackupChannel(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting BackupChannel %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *BackupChannelAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating BackupChannel", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := GKEBackupBackupChannelSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateBackupChannelRequest{
		Parent:          a.id.Parent().String(),
		BackupChannelId: a.id.ID(),
		BackupChannel:   resource,
	}
	op, err := a.gcpClient.CreateBackupChannel(ctx, req)
	if err != nil {
		return fmt.Errorf("creating BackupChannel %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create BackupChannel %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created BackupChannel", "name", a.id)

	status := &krm.GKEBackupBackupChannelStatus{}
	status.ObservedState = GKEBackupBackupChannelObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *BackupChannelAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating BackupChannel", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := GKEBackupBackupChannelSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareBackupChannel(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateBackupChannelRequest{
			BackupChannel: desired,
			UpdateMask:    updateMask,
		}
		op, err := a.gcpClient.UpdateBackupChannel(ctx, req)
		if err != nil {
			return fmt.Errorf("updating BackupChannel %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update BackupChannel %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated BackupChannel", "name", a.id)

		status := &krm.GKEBackupBackupChannelStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = GKEBackupBackupChannelObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareBackupChannel(ctx context.Context, actual, desired *pb.BackupChannel) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, GKEBackupBackupChannelSpec_FromProto, GKEBackupBackupChannelSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	maskedActual.Labels = actual.Labels
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *BackupChannelAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.GKEBackupBackupChannel{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(GKEBackupBackupChannelSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = a.id.Location
	obj.Spec.ResourceID = direct.LazyPtr(a.id.ID())

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.GKEBackupBackupChannelGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *BackupChannelAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting BackupChannel", "name", a.id)

	req := &pb.DeleteBackupChannelRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteBackupChannel(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting BackupChannel %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted BackupChannel", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete BackupChannel %s: %w", a.id, err)
		}
	}
	return true, nil
}
