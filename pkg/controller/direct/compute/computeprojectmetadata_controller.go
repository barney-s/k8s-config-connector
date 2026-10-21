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

package compute

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/compute/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/compute/apiv1"
	pb "cloud.google.com/go/compute/apiv1/computepb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.ComputeProjectMetadataGVK, NewComputeProjectMetadataModel)
}

func NewComputeProjectMetadataModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelComputeProjectMetadata{config: *config}, nil
}

var _ directbase.Model = &modelComputeProjectMetadata{}
var _ = strings.TrimPrefix

type modelComputeProjectMetadata struct {
	config config.ControllerConfig
}

func (m *modelComputeProjectMetadata) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.ComputeProjectMetadata{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.ComputeProjectMetadataIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ComputeProjectMetadataAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelComputeProjectMetadata) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.ComputeProjectMetadataIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ComputeProjectMetadataAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelComputeProjectMetadata) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ComputeProjectMetadata client: %w", err)
	}
	return gcpClient, err
}

type ComputeProjectMetadataAdapter struct {
	id        *krm.ComputeProjectMetadataIdentity
	gcpClient *gcp.Client
	desired   *krm.ComputeProjectMetadata
	actual    *pb.ComputeProjectMetadata
}

var _ directbase.Adapter = &ComputeProjectMetadataAdapter{}

func (a *ComputeProjectMetadataAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ComputeProjectMetadata", "name", a.id)

	req := &pb.GetComputeProjectMetadataRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetComputeProjectMetadata(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ComputeProjectMetadata %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ComputeProjectMetadataAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ComputeProjectMetadata", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := ComputeProjectMetadataSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateComputeProjectMetadataRequest{
		Parent:                   a.id.ParentString(),
		ComputeProjectMetadataId: a.id.ComputeProjectMetadata,
		ComputeProjectMetadata:   resource,
	}
	op, err := a.gcpClient.CreateComputeProjectMetadata(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ComputeProjectMetadata %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create ComputeProjectMetadata %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ComputeProjectMetadata", "name", a.id)

	status := &krm.ComputeProjectMetadataStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ComputeProjectMetadataAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ComputeProjectMetadata", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := ComputeProjectMetadataSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareComputeProjectMetadata(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("ComputeProjectMetadata resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.ComputeProjectMetadataStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareComputeProjectMetadata(ctx context.Context, actual, desired *pb.ComputeProjectMetadata) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, ComputeProjectMetadataSpec_FromProto, ComputeProjectMetadataSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *ComputeProjectMetadataAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.ComputeProjectMetadata{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(ComputeProjectMetadataSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ComputeProjectMetadata)
	u.SetGroupVersionKind(krm.ComputeProjectMetadataGVK)
	return u, nil
}

func (a *ComputeProjectMetadataAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deletion not supported for ComputeProjectMetadata, removing finalizer without deleting GCP resource", "name", a.id)
	return true, nil
}
