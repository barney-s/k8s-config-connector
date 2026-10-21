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
	registry.RegisterModel(krm.ComputeTargetTCPProxyGVK, NewComputeTargetTCPProxyModel)
}

func NewComputeTargetTCPProxyModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelComputeTargetTCPProxy{config: *config}, nil
}

var _ directbase.Model = &modelComputeTargetTCPProxy{}
var _ = strings.TrimPrefix

type modelComputeTargetTCPProxy struct {
	config config.ControllerConfig
}

func (m *modelComputeTargetTCPProxy) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.ComputeTargetTCPProxy{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.TargetTCPProxyIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ComputeTargetTCPProxyAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelComputeTargetTCPProxy) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelComputeTargetTCPProxy) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ComputeTargetTCPProxy client: %w", err)
	}
	return gcpClient, err
}

type ComputeTargetTCPProxyAdapter struct {
	id        *krm.TargetTCPProxyIdentity
	gcpClient *gcp.Client
	desired   *krm.ComputeTargetTCPProxy
	actual    *computepb.ComputeTargetTCPProxy
}

var _ directbase.Adapter = &ComputeTargetTCPProxyAdapter{}

func (a *ComputeTargetTCPProxyAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ComputeTargetTCPProxy", "name", a.id)

	req := &computepb.GetComputeTargetTCPProxyRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetComputeTargetTCPProxy(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ComputeTargetTCPProxy %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ComputeTargetTCPProxyAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ComputeTargetTCPProxy", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := ComputeTargetTCPProxySpec_v1beta1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &computepb.CreateComputeTargetTCPProxyRequest{
		Parent:                  a.id.ParentString(),
		ComputeTargetTCPProxyId: a.id.ID(),
		ComputeTargetTCPProxy:   resource,
	}
	op, err := a.gcpClient.CreateComputeTargetTCPProxy(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ComputeTargetTCPProxy %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create ComputeTargetTCPProxy %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ComputeTargetTCPProxy", "name", a.id)

	status := &krm.ComputeTargetTCPProxyStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ComputeTargetTCPProxyAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ComputeTargetTCPProxy", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := ComputeTargetTCPProxySpec_v1beta1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareComputeTargetTCPProxy(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("ComputeTargetTCPProxy resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.ComputeTargetTCPProxyStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareComputeTargetTCPProxy(ctx context.Context, actual, desired *computepb.ComputeTargetTCPProxy) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, ComputeTargetTCPProxySpec_v1beta1_FromProto, ComputeTargetTCPProxySpec_v1beta1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *ComputeTargetTCPProxyAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.ComputeTargetTCPProxy{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(ComputeTargetTCPProxySpec_v1beta1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.ComputeTargetTCPProxyGVK)
	return u, nil
}

func (a *ComputeTargetTCPProxyAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deletion not supported for ComputeTargetTCPProxy, removing finalizer without deleting GCP resource", "name", a.id)
	return true, nil
}
