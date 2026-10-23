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

package storageinsights

import (
	"context"
	"fmt"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/storageinsights/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/export"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/label"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/storageinsights/apiv1"
	pb "cloud.google.com/go/storageinsights/apiv1/storageinsightspb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.StorageInsightsReportConfigGVK, NewReportConfigModel)
}

func NewReportConfigModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelReportConfig{config: *config}, nil
}

var _ directbase.Model = &modelReportConfig{}
var _ = strings.TrimPrefix

type modelReportConfig struct {
	config config.ControllerConfig
}

func (m *modelReportConfig) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.StorageInsightsReportConfig{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.StorageInsightsReportConfigIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ReportConfigAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelReportConfig) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.StorageInsightsReportConfigIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ReportConfigAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelReportConfig) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ReportConfig client: %w", err)
	}
	return gcpClient, err
}

type ReportConfigAdapter struct {
	id        *krm.StorageInsightsReportConfigIdentity
	gcpClient *gcp.Client
	desired   *krm.StorageInsightsReportConfig
	actual    *pb.ReportConfig
}

var _ directbase.Adapter = &ReportConfigAdapter{}

func (a *ReportConfigAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ReportConfig", "name", a.id)
	if a.id.ReportConfig == "" {
		return false, nil
	}

	req := &pb.GetReportConfigRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetReportConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ReportConfig %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ReportConfigAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ReportConfig", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := StorageInsightsReportConfigSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateReportConfigRequest{
		Parent:       a.id.ParentString(),
		ReportConfig: resource,
	}
	created, err := a.gcpClient.CreateReportConfig(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ReportConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ReportConfig", "name", a.id)

	status := &krm.StorageInsightsReportConfigStatus{}
	status.ObservedState = StorageInsightsReportConfigObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(created.GetName())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ReportConfigAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ReportConfig", "name", a.id)

	desired, diffs, updateMask, err := compareReportConfig(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateReportConfigRequest{
			ReportConfig: desired,
			UpdateMask:   updateMask,
		}
		updated, err := a.gcpClient.UpdateReportConfig(ctx, req)
		if err != nil {
			return fmt.Errorf("updating ReportConfig %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated ReportConfig", "name", a.id)

		status := &krm.StorageInsightsReportConfigStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = StorageInsightsReportConfigObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareReportConfig(ctx context.Context, u *unstructured.Unstructured, actual *pb.ReportConfig, desiredSpec *krm.StorageInsightsReportConfigSpec) (*pb.ReportConfig, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	return common.CompareGreenfieldSpecAndLabels(
		ctx,
		u,
		desiredSpec,
		actual,
		"labels",
		StorageInsightsReportConfigSpec_FromProto,
		StorageInsightsReportConfigSpec_ToProto,
		nil,
	)
}

func (a *ReportConfigAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.StorageInsightsReportConfig{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(StorageInsightsReportConfigSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = &a.id.Location
	obj.Spec.ResourceID = direct.PtrTo(a.id.ReportConfig)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ReportConfig)
	u.SetGroupVersionKind(krm.StorageInsightsReportConfigGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *ReportConfigAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting ReportConfig", "name", a.id)

	req := &pb.DeleteReportConfigRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteReportConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting ReportConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted ReportConfig", "name", a.id)
	return true, nil
}
