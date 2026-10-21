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

package vision

import (
	"context"
	"fmt"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/vision/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/vision/v2/apiv1"
	pb "cloud.google.com/go/vision/v2/apiv1/visionpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.VisionProductGVK, NewProductModel)
}

func NewProductModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelProduct{config: *config}, nil
}

var _ directbase.Model = &modelProduct{}
var _ = strings.TrimPrefix

type modelProduct struct {
	config config.ControllerConfig
}

func (m *modelProduct) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.VisionProduct{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.VisionProductIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ProductAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelProduct) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.VisionProductIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ProductAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelProduct) client(ctx context.Context) (*gcp.ProductSearchClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewProductSearchRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Product client: %w", err)
	}
	return gcpClient, err
}

type ProductAdapter struct {
	id        *krm.VisionProductIdentity
	gcpClient *gcp.ProductSearchClient
	desired   *krm.VisionProduct
	actual    *pb.Product
}

var _ directbase.Adapter = &ProductAdapter{}

func (a *ProductAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Product", "name", a.id)

	req := &pb.GetProductRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetProduct(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Product %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ProductAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Product", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := VisionProductSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateProductRequest{
		Parent:    a.id.ParentString(),
		ProductId: a.id.Product,
		Product:   resource,
	}
	created, err := a.gcpClient.CreateProduct(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Product %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Product", "name", a.id)

	status := &krm.VisionProductStatus{}
	status.ObservedState = VisionProductObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ProductAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Product", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := VisionProductSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareProduct(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateProductRequest{
			Product:    desired,
			UpdateMask: updateMask,
		}
		updated, err := a.gcpClient.UpdateProduct(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Product %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Product", "name", a.id)

		status := &krm.VisionProductStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = VisionProductObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareProduct(ctx context.Context, actual, desired *pb.Product) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, VisionProductSpec_FromProto, VisionProductSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *ProductAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.VisionProduct{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(VisionProductSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = a.id.Location
	obj.Spec.ResourceID = direct.LazyPtr(a.id.Product)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Product)
	u.SetGroupVersionKind(krm.VisionProductGVK)
	return u, nil
}

func (a *ProductAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Product", "name", a.id)

	req := &pb.DeleteProductRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteProduct(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Product %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Product", "name", a.id)
	return true, nil
}
