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
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/compute/v1alpha1"
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
	registry.RegisterModel(krm.ComputeBackendServiceSignedURLKeyGVK, NewComputeBackendServiceSignedURLKeyModel)
}

func NewComputeBackendServiceSignedURLKeyModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelComputeBackendServiceSignedURLKey{config: *config}, nil
}

var _ directbase.Model = &modelComputeBackendServiceSignedURLKey{}
var _ = strings.TrimPrefix

type modelComputeBackendServiceSignedURLKey struct {
	config config.ControllerConfig
}

func (m *modelComputeBackendServiceSignedURLKey) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.ComputeBackendServiceSignedURLKey{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.ComputeBackendServiceSignedURLKeyIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ComputeBackendServiceSignedURLKeyAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelComputeBackendServiceSignedURLKey) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelComputeBackendServiceSignedURLKey) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ComputeBackendServiceSignedURLKey client: %w", err)
	}
	return gcpClient, err
}

type ComputeBackendServiceSignedURLKeyAdapter struct {
	id        *krm.ComputeBackendServiceSignedURLKeyIdentity
	gcpClient *gcp.Client
	desired   *krm.ComputeBackendServiceSignedURLKey
	actual    *pb.ComputeBackendServiceSignedURLKey
}

var _ directbase.Adapter = &ComputeBackendServiceSignedURLKeyAdapter{}

func (a *ComputeBackendServiceSignedURLKeyAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ComputeBackendServiceSignedURLKey", "name", a.id)

	req := &pb.GetComputeBackendServiceSignedURLKeyRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetComputeBackendServiceSignedURLKey(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ComputeBackendServiceSignedURLKey %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ComputeBackendServiceSignedURLKeyAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ComputeBackendServiceSignedURLKey", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := ComputeBackendServiceSignedURLKeySpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateComputeBackendServiceSignedURLKeyRequest{
		Parent:                              a.id.ParentString(),
		ComputeBackendServiceSignedURLKeyId: a.id.BackendService,
		ComputeBackendServiceSignedURLKey:   resource,
	}
	op, err := a.gcpClient.CreateComputeBackendServiceSignedURLKey(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ComputeBackendServiceSignedURLKey %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create ComputeBackendServiceSignedURLKey %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ComputeBackendServiceSignedURLKey", "name", a.id)

	status := &krm.ComputeBackendServiceSignedURLKeyStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ComputeBackendServiceSignedURLKeyAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ComputeBackendServiceSignedURLKey", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := ComputeBackendServiceSignedURLKeySpec_v1alpha1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareComputeBackendServiceSignedURLKey(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("ComputeBackendServiceSignedURLKey resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.ComputeBackendServiceSignedURLKeyStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareComputeBackendServiceSignedURLKey(ctx context.Context, actual, desired *pb.ComputeBackendServiceSignedURLKey) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, ComputeBackendServiceSignedURLKeySpec_v1alpha1_FromProto, ComputeBackendServiceSignedURLKeySpec_v1alpha1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *ComputeBackendServiceSignedURLKeyAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.ComputeBackendServiceSignedURLKey{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(ComputeBackendServiceSignedURLKeySpec_v1alpha1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.BackendService)
	u.SetGroupVersionKind(krm.ComputeBackendServiceSignedURLKeyGVK)
	return u, nil
}

func (a *ComputeBackendServiceSignedURLKeyAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deletion not supported for ComputeBackendServiceSignedURLKey, removing finalizer without deleting GCP resource", "name", a.id)
	return true, nil
}
