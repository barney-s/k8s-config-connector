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

package backupdr

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/backupdr/v1alpha1"
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

	gcp "cloud.google.com/go/backupdr/apiv1"
	pb "cloud.google.com/go/backupdr/apiv1/backupdrpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.BackupDRManagementServerGVK, NewManagementServerModel)
}

func NewManagementServerModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelManagementServer{config: *config}, nil
}

var _ directbase.Model = &modelManagementServer{}
var _ = strings.TrimPrefix

type modelManagementServer struct {
	config config.ControllerConfig
}

func (m *modelManagementServer) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.BackupDRManagementServer{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.ManagementServerIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ManagementServerAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelManagementServer) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.ManagementServerIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ManagementServerAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelManagementServer) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ManagementServer client: %w", err)
	}
	return gcpClient, err
}

type ManagementServerAdapter struct {
	id        *krm.ManagementServerIdentity
	gcpClient *gcp.Client
	desired   *krm.BackupDRManagementServer
	actual    *pb.ManagementServer
}

var _ directbase.Adapter = &ManagementServerAdapter{}

func (a *ManagementServerAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ManagementServer", "name", a.id)

	req := &pb.GetManagementServerRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetManagementServer(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ManagementServer %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ManagementServerAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ManagementServer", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := BackupDRManagementServerSpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateManagementServerRequest{
		Parent:             a.id.ParentString(),
		ManagementServerId: a.id.ManagementServer,
		ManagementServer:   resource,
	}
	op, err := a.gcpClient.CreateManagementServer(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ManagementServer %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create ManagementServer %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ManagementServer", "name", a.id)

	status := &krm.BackupDRManagementServerStatus{}
	status.ObservedState = BackupDRManagementServerObservedState_v1alpha1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ManagementServerAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ManagementServer", "name", a.id)

	desired, diffs, updateMask, err := compareManagementServer(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	_ = desired
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("BackupDRManagementServer resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.BackupDRManagementServerStatus{}
	mapCtx := &direct.MapContext{}
	status.ObservedState = BackupDRManagementServerObservedState_v1alpha1_FromProto(mapCtx, a.actual)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareManagementServer(ctx context.Context, u *unstructured.Unstructured, actual *pb.ManagementServer, desiredSpec *krm.BackupDRManagementServerSpec) (*pb.ManagementServer, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	return common.CompareGreenfieldSpecAndLabels(
		ctx,
		u,
		desiredSpec,
		actual,
		"labels",
		BackupDRManagementServerSpec_v1alpha1_FromProto,
		BackupDRManagementServerSpec_v1alpha1_ToProto,
		nil,
	)
}

func (a *ManagementServerAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.BackupDRManagementServer{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(BackupDRManagementServerSpec_v1alpha1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = a.id.Location
	obj.Spec.ResourceID = direct.LazyPtr(a.id.ManagementServer)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ManagementServer)
	u.SetGroupVersionKind(krm.BackupDRManagementServerGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *ManagementServerAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting ManagementServer", "name", a.id)

	req := &pb.DeleteManagementServerRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteManagementServer(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting ManagementServer %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted ManagementServer", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete ManagementServer %s: %w", a.id, err)
		}
	}
	return true, nil
}
