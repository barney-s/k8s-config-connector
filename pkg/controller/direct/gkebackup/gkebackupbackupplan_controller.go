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
	registry.RegisterModel(krm.GKEBackupBackupPlanGVK, NewBackupPlanModel)
}

func NewBackupPlanModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelBackupPlan{config: *config}, nil
}

var _ directbase.Model = &modelBackupPlan{}
var _ = strings.TrimPrefix

type modelBackupPlan struct {
	config config.ControllerConfig
}

func (m *modelBackupPlan) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.GKEBackupBackupPlan{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id, err := krm.NewBackupPlanIdentity(ctx, reader, obj)
	if err != nil {
		return nil, err
	}
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &BackupPlanAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelBackupPlan) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelBackupPlan) client(ctx context.Context) (*gcp.BackupForGKEClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewBackupForGKERESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building BackupPlan client: %w", err)
	}
	return gcpClient, err
}

type BackupPlanAdapter struct {
	id        *krm.BackupPlanIdentity
	gcpClient *gcp.BackupForGKEClient
	desired   *krm.GKEBackupBackupPlan
	actual    *pb.BackupPlan
}

var _ directbase.Adapter = &BackupPlanAdapter{}

func (a *BackupPlanAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting BackupPlan", "name", a.id)

	req := &pb.GetBackupPlanRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetBackupPlan(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting BackupPlan %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *BackupPlanAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating BackupPlan", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := GKEBackupBackupPlanSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateBackupPlanRequest{
		Parent:       a.id.Parent().String(),
		BackupPlanId: a.id.ID(),
		BackupPlan:   resource,
	}
	op, err := a.gcpClient.CreateBackupPlan(ctx, req)
	if err != nil {
		return fmt.Errorf("creating BackupPlan %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create BackupPlan %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created BackupPlan", "name", a.id)

	status := &krm.GKEBackupBackupPlanStatus{}
	status.ObservedState = GKEBackupBackupPlanObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *BackupPlanAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating BackupPlan", "name", a.id)

	desired, diffs, updateMask, err := compareBackupPlan(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateBackupPlanRequest{
			BackupPlan: desired,
			UpdateMask: updateMask,
		}
		op, err := a.gcpClient.UpdateBackupPlan(ctx, req)
		if err != nil {
			return fmt.Errorf("updating BackupPlan %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update BackupPlan %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated BackupPlan", "name", a.id)

		status := &krm.GKEBackupBackupPlanStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = GKEBackupBackupPlanObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareBackupPlan(ctx context.Context, u *unstructured.Unstructured, actual *pb.BackupPlan, desiredSpec *krm.GKEBackupBackupPlanSpec) (*pb.BackupPlan, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	return common.CompareGreenfieldSpecAndLabels(
		ctx,
		u,
		desiredSpec,
		actual,
		"labels",
		GKEBackupBackupPlanSpec_FromProto,
		GKEBackupBackupPlanSpec_ToProto,
		nil,
	)
}

func (a *BackupPlanAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.GKEBackupBackupPlan{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(GKEBackupBackupPlanSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Parent().ProjectID}
	obj.Spec.Location = a.id.Parent().Location

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.GKEBackupBackupPlanGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *BackupPlanAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting BackupPlan", "name", a.id)

	req := &pb.DeleteBackupPlanRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteBackupPlan(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting BackupPlan %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted BackupPlan", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete BackupPlan %s: %w", a.id, err)
		}
	}
	return true, nil
}
