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

package datacatalog

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/datacatalog/v1alpha1"
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

	gcp "cloud.google.com/go/datacatalog/apiv1"
	pb "cloud.google.com/go/datacatalog/apiv1/datacatalogpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.DataCatalogEntryGVK, NewEntryModel)
}

func NewEntryModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelEntry{config: *config}, nil
}

var _ directbase.Model = &modelEntry{}
var _ = strings.TrimPrefix

type modelEntry struct {
	config config.ControllerConfig
}

func (m *modelEntry) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.DataCatalogEntry{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.EntryIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &EntryAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelEntry) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelEntry) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Entry client: %w", err)
	}
	return gcpClient, err
}

type EntryAdapter struct {
	id        *krm.EntryIdentity
	gcpClient *gcp.Client
	desired   *krm.DataCatalogEntry
	actual    *pb.Entry
}

var _ directbase.Adapter = &EntryAdapter{}

func (a *EntryAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Entry", "name", a.id)

	req := &pb.GetEntryRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetEntry(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Entry %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *EntryAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Entry", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := DataCatalogEntrySpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateEntryRequest{
		Parent:  a.id.Parent().String(),
		EntryId: a.id.ID(),
		Entry:   resource,
	}
	created, err := a.gcpClient.CreateEntry(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Entry %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Entry", "name", a.id)

	status := &krm.DataCatalogEntryStatus{}
	status.ObservedState = DataCatalogEntryObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *EntryAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Entry", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := DataCatalogEntrySpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareEntry(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateEntryRequest{
			Entry:      desired,
			UpdateMask: updateMask,
		}
		updated, err := a.gcpClient.UpdateEntry(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Entry %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Entry", "name", a.id)

		status := &krm.DataCatalogEntryStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = DataCatalogEntryObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareEntry(ctx context.Context, actual, desired *pb.Entry) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, DataCatalogEntrySpec_FromProto, DataCatalogEntrySpec_ToProto)
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

func (a *EntryAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.DataCatalogEntry{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(DataCatalogEntrySpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.DataCatalogEntryGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *EntryAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Entry", "name", a.id)

	req := &pb.DeleteEntryRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteEntry(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Entry %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Entry", "name", a.id)
	return true, nil
}
