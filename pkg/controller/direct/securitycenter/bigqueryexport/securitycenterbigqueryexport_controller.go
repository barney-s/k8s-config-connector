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

package bigqueryexport

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/securitycenter/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/securitycenter/apiv1"
	pb "cloud.google.com/go/securitycenter/apiv1/securitycenterpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.SecurityCenterBigQueryExportGVK, NewBigQueryExportModel)
}

func NewBigQueryExportModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelBigQueryExport{config: *config}, nil
}

var _ directbase.Model = &modelBigQueryExport{}
var _ = strings.TrimPrefix

type modelBigQueryExport struct {
	config config.ControllerConfig
}

func (m *modelBigQueryExport) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.SecurityCenterBigQueryExport{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.SecurityCenterBigQueryExportIdentity)

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &BigQueryExportAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelBigQueryExport) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelBigQueryExport) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building BigQueryExport client: %w", err)
	}
	return gcpClient, err
}

type BigQueryExportAdapter struct {
	id        *krm.SecurityCenterBigQueryExportIdentity
	gcpClient *gcp.Client
	desired   *krm.SecurityCenterBigQueryExport
	actual    *pb.BigQueryExport
}

var _ directbase.Adapter = &BigQueryExportAdapter{}

func (a *BigQueryExportAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting BigQueryExport", "name", a.id)

	req := &pb.GetBigQueryExportRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetBigQueryExport(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting BigQueryExport %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *BigQueryExportAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating BigQueryExport", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := SecurityCenterBigQueryExportSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateBigQueryExportRequest{
		Parent:           a.id.ParentString(),
		BigQueryExportId: a.id.Organization,
		BigQueryExport:   resource,
	}
	created, err := a.gcpClient.CreateBigQueryExport(ctx, req)
	if err != nil {
		return fmt.Errorf("creating BigQueryExport %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created BigQueryExport", "name", a.id)

	status := &krm.SecurityCenterBigQueryExportStatus{}
	status.ObservedState = SecurityCenterBigQueryExportObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *BigQueryExportAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating BigQueryExport", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := SecurityCenterBigQueryExportSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareBigQueryExport(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateBigQueryExportRequest{
			BigQueryExport: desired,
			UpdateMask:     updateMask,
		}
		updated, err := a.gcpClient.UpdateBigQueryExport(ctx, req)
		if err != nil {
			return fmt.Errorf("updating BigQueryExport %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated BigQueryExport", "name", a.id)

		status := &krm.SecurityCenterBigQueryExportStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = SecurityCenterBigQueryExportObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareBigQueryExport(ctx context.Context, actual, desired *pb.BigQueryExport) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, SecurityCenterBigQueryExportSpec_FromProto, SecurityCenterBigQueryExportSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *BigQueryExportAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.SecurityCenterBigQueryExport{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(SecurityCenterBigQueryExportSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.Location = a.id.Location

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Organization)
	u.SetGroupVersionKind(krm.SecurityCenterBigQueryExportGVK)
	return u, nil
}

func (a *BigQueryExportAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting BigQueryExport", "name", a.id)

	req := &pb.DeleteBigQueryExportRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteBigQueryExport(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting BigQueryExport %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted BigQueryExport", "name", a.id)
	return true, nil
}
