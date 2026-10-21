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

package bigtable

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/bigtable/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/bigtable"
	bigtablepb "cloud.google.com/go/bigtable/admin/apiv2/adminpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.BigtableLogicalViewGVK, NewBigtableLogicalViewModel)
}

func NewBigtableLogicalViewModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelBigtableLogicalView{config: *config}, nil
}

var _ directbase.Model = &modelBigtableLogicalView{}
var _ = strings.TrimPrefix

type modelBigtableLogicalView struct {
	config config.ControllerConfig
}

func (m *modelBigtableLogicalView) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.BigtableLogicalView{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.LogicalViewIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &BigtableLogicalViewAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelBigtableLogicalView) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelBigtableLogicalView) client(ctx context.Context) (*gcp.InstanceAdminClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.GRPCClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewInstanceAdminClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building BigtableLogicalView client: %w", err)
	}
	return gcpClient, err
}

type BigtableLogicalViewAdapter struct {
	id        *krm.LogicalViewIdentity
	gcpClient *gcp.InstanceAdminClient
	desired   *krm.BigtableLogicalView
	actual    *bigtablepb.BigtableLogicalView
}

var _ directbase.Adapter = &BigtableLogicalViewAdapter{}

func (a *BigtableLogicalViewAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting BigtableLogicalView", "name", a.id)

	req := &bigtablepb.GetBigtableLogicalViewRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetBigtableLogicalView(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting BigtableLogicalView %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *BigtableLogicalViewAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating BigtableLogicalView", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := BigtableLogicalViewSpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &bigtablepb.CreateBigtableLogicalViewRequest{
		Parent:                a.id.ParentString(),
		BigtableLogicalViewId: a.id.ID(),
		BigtableLogicalView:   resource,
	}
	created, err := a.gcpClient.CreateBigtableLogicalView(ctx, req)
	if err != nil {
		return fmt.Errorf("creating BigtableLogicalView %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created BigtableLogicalView", "name", a.id)

	status := &krm.BigtableLogicalViewStatus{}
	status.ObservedState = BigtableLogicalViewObservedState_v1alpha1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *BigtableLogicalViewAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating BigtableLogicalView", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := BigtableLogicalViewSpec_v1alpha1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareBigtableLogicalView(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("BigtableLogicalView resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.BigtableLogicalViewStatus{}
	mapCtx = &direct.MapContext{}
	status.ObservedState = BigtableLogicalViewObservedState_v1alpha1_FromProto(mapCtx, a.actual)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareBigtableLogicalView(ctx context.Context, actual, desired *bigtablepb.BigtableLogicalView) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, BigtableLogicalViewSpec_v1alpha1_FromProto, BigtableLogicalViewSpec_v1alpha1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *BigtableLogicalViewAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.BigtableLogicalView{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(BigtableLogicalViewSpec_v1alpha1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.BigtableLogicalViewGVK)
	return u, nil
}

func (a *BigtableLogicalViewAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deletion not supported for BigtableLogicalView, removing finalizer without deleting GCP resource", "name", a.id)
	return true, nil
}
