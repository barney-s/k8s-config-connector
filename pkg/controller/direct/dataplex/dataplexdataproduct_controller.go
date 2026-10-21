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

package dataplex

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/dataplex/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/dataplex/apiv1"
	pb "cloud.google.com/go/dataplex/apiv1/dataplexpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.DataplexDataProductGVK, NewDataProductModel)
}

func NewDataProductModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelDataProduct{config: *config}, nil
}

var _ directbase.Model = &modelDataProduct{}
var _ = strings.TrimPrefix

type modelDataProduct struct {
	config config.ControllerConfig
}

func (m *modelDataProduct) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.DataplexDataProduct{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.DataProductIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &DataProductAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelDataProduct) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelDataProduct) client(ctx context.Context) (*gcp.DataProductClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building DataProduct client: %w", err)
	}
	return gcpClient, err
}

type DataProductAdapter struct {
	id        *krm.DataProductIdentity
	gcpClient *gcp.DataProductClient
	desired   *krm.DataplexDataProduct
	actual    *pb.DataProduct
}

var _ directbase.Adapter = &DataProductAdapter{}

func (a *DataProductAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting DataProduct", "name", a.id)

	req := &pb.GetDataProductRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetDataProduct(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting DataProduct %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *DataProductAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating DataProduct", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := DataplexDataProductSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateDataProductRequest{
		Parent:        a.id.ParentString(),
		DataProductId: a.id.DataProduct,
		DataProduct:   resource,
	}
	op, err := a.gcpClient.CreateDataProduct(ctx, req)
	if err != nil {
		return fmt.Errorf("creating DataProduct %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create DataProduct %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created DataProduct", "name", a.id)

	status := &krm.DataplexDataProductStatus{}
	status.ObservedState = DataplexDataProductObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *DataProductAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating DataProduct", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := DataplexDataProductSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareDataProduct(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateDataProductRequest{
			DataProduct: desired,
			UpdateMask:  updateMask,
		}
		op, err := a.gcpClient.UpdateDataProduct(ctx, req)
		if err != nil {
			return fmt.Errorf("updating DataProduct %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update DataProduct %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated DataProduct", "name", a.id)

		status := &krm.DataplexDataProductStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = DataplexDataProductObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareDataProduct(ctx context.Context, actual, desired *pb.DataProduct) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, DataplexDataProductSpec_FromProto, DataplexDataProductSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *DataProductAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.DataplexDataProduct{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(DataplexDataProductSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.DataProduct)
	u.SetGroupVersionKind(krm.DataplexDataProductGVK)
	return u, nil
}

func (a *DataProductAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting DataProduct", "name", a.id)

	req := &pb.DeleteDataProductRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteDataProduct(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting DataProduct %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted DataProduct", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete DataProduct %s: %w", a.id, err)
		}
	}
	return true, nil
}
