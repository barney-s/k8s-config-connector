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
	computepb "cloud.google.com/go/compute/apiv1/computepb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.ComputeHTTPHealthCheckGVK, NewComputeHTTPHealthCheckModel)
}

func NewComputeHTTPHealthCheckModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelComputeHTTPHealthCheck{config: *config}, nil
}

var _ directbase.Model = &modelComputeHTTPHealthCheck{}
var _ = strings.TrimPrefix

type modelComputeHTTPHealthCheck struct {
	config config.ControllerConfig
}

func (m *modelComputeHTTPHealthCheck) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.ComputeHTTPHealthCheck{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*v1beta1.ComputeHTTPHealthCheckIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ComputeHTTPHealthCheckAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelComputeHTTPHealthCheck) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelComputeHTTPHealthCheck) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ComputeHTTPHealthCheck client: %w", err)
	}
	return gcpClient, err
}

type ComputeHTTPHealthCheckAdapter struct {
	id        *v1beta1.ComputeHTTPHealthCheckIdentity
	gcpClient *gcp.Client
	desired   *krm.ComputeHTTPHealthCheck
	actual    *computepb.ComputeHTTPHealthCheck
}

var _ directbase.Adapter = &ComputeHTTPHealthCheckAdapter{}

func (a *ComputeHTTPHealthCheckAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ComputeHTTPHealthCheck", "name", a.id)

	req := &computepb.GetComputeHTTPHealthCheckRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetComputeHTTPHealthCheck(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ComputeHTTPHealthCheck %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ComputeHTTPHealthCheckAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ComputeHTTPHealthCheck", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := ComputeHTTPHealthCheckSpec_v1beta1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &computepb.CreateComputeHTTPHealthCheckRequest{
		Parent:                   a.id.ParentString(),
		ComputeHTTPHealthCheckId: a.id.HttpHealthCheck,
		ComputeHTTPHealthCheck:   resource,
	}
	op, err := a.gcpClient.CreateComputeHTTPHealthCheck(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ComputeHTTPHealthCheck %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create ComputeHTTPHealthCheck %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ComputeHTTPHealthCheck", "name", a.id)

	status := &krm.ComputeHTTPHealthCheckStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ComputeHTTPHealthCheckAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ComputeHTTPHealthCheck", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := ComputeHTTPHealthCheckSpec_v1beta1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareComputeHTTPHealthCheck(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("ComputeHTTPHealthCheck resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.ComputeHTTPHealthCheckStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareComputeHTTPHealthCheck(ctx context.Context, actual, desired *computepb.ComputeHTTPHealthCheck) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, ComputeHTTPHealthCheckSpec_v1beta1_FromProto, ComputeHTTPHealthCheckSpec_v1beta1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *ComputeHTTPHealthCheckAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.ComputeHTTPHealthCheck{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(ComputeHTTPHealthCheckSpec_v1beta1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.HttpHealthCheck)
	u.SetGroupVersionKind(krm.ComputeHTTPHealthCheckGVK)
	return u, nil
}

func (a *ComputeHTTPHealthCheckAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deletion not supported for ComputeHTTPHealthCheck, removing finalizer without deleting GCP resource", "name", a.id)
	return true, nil
}
