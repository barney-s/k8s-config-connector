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
	registry.RegisterModel(krm.GKEBackupRestorePlanGVK, NewRestorePlanModel)
}

func NewRestorePlanModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelRestorePlan{config: *config}, nil
}

var _ directbase.Model = &modelRestorePlan{}
var _ = strings.TrimPrefix

type modelRestorePlan struct {
	config config.ControllerConfig
}

func (m *modelRestorePlan) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.GKEBackupRestorePlan{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id, err := krm.NewRestorePlanIdentity(ctx, reader, obj)
	if err != nil {
		return nil, err
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &RestorePlanAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelRestorePlan) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelRestorePlan) client(ctx context.Context) (*gcp.BackupForGKEClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewBackupForGKERESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building RestorePlan client: %w", err)
	}
	return gcpClient, err
}

type RestorePlanAdapter struct {
	id        *krm.RestorePlanIdentity
	gcpClient *gcp.BackupForGKEClient
	desired   *krm.GKEBackupRestorePlan
	actual    *pb.RestorePlan
}

var _ directbase.Adapter = &RestorePlanAdapter{}

func (a *RestorePlanAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting RestorePlan", "name", a.id)

	req := &pb.GetRestorePlanRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetRestorePlan(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting RestorePlan %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *RestorePlanAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating RestorePlan", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := GKEBackupRestorePlanSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateRestorePlanRequest{
		Parent:        a.id.Parent().String(),
		RestorePlanId: a.id.ID(),
		RestorePlan:   resource,
	}
	op, err := a.gcpClient.CreateRestorePlan(ctx, req)
	if err != nil {
		return fmt.Errorf("creating RestorePlan %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create RestorePlan %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created RestorePlan", "name", a.id)

	status := &krm.GKEBackupRestorePlanStatus{}
	status.ObservedState = GKEBackupRestorePlanObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *RestorePlanAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating RestorePlan", "name", a.id)

	desired, diffs, updateMask, err := compareRestorePlan(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateRestorePlanRequest{
			RestorePlan: desired,
			UpdateMask:  updateMask,
		}
		op, err := a.gcpClient.UpdateRestorePlan(ctx, req)
		if err != nil {
			return fmt.Errorf("updating RestorePlan %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update RestorePlan %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated RestorePlan", "name", a.id)

		status := &krm.GKEBackupRestorePlanStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = GKEBackupRestorePlanObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareRestorePlan(ctx context.Context, u *unstructured.Unstructured, actual *pb.RestorePlan, desiredSpec *krm.GKEBackupRestorePlanSpec) (*pb.RestorePlan, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	return common.CompareGreenfieldSpecAndLabels(
		ctx,
		u,
		desiredSpec,
		actual,
		"labels",
		GKEBackupRestorePlanSpec_FromProto,
		GKEBackupRestorePlanSpec_ToProto,
		nil,
	)
}

func (a *RestorePlanAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.GKEBackupRestorePlan{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(GKEBackupRestorePlanSpec_FromProto(mapCtx, a.actual))
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
	u.SetGroupVersionKind(krm.GKEBackupRestorePlanGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *RestorePlanAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting RestorePlan", "name", a.id)

	req := &pb.DeleteRestorePlanRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteRestorePlan(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting RestorePlan %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted RestorePlan", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete RestorePlan %s: %w", a.id, err)
		}
	}
	return true, nil
}
