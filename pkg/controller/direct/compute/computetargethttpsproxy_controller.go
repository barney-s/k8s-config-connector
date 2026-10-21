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
	registry.RegisterModel(krm.ComputeTargetHTTPSProxyGVK, NewComputeTargetHTTPSProxyModel)
}

func NewComputeTargetHTTPSProxyModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelComputeTargetHTTPSProxy{config: *config}, nil
}

var _ directbase.Model = &modelComputeTargetHTTPSProxy{}
var _ = strings.TrimPrefix

type modelComputeTargetHTTPSProxy struct {
	config config.ControllerConfig
}

func (m *modelComputeTargetHTTPSProxy) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.ComputeTargetHTTPSProxy{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*v1beta1.ComputeTargetHTTPSProxyIdentity)

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ComputeTargetHTTPSProxyAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelComputeTargetHTTPSProxy) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelComputeTargetHTTPSProxy) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ComputeTargetHTTPSProxy client: %w", err)
	}
	return gcpClient, err
}

type ComputeTargetHTTPSProxyAdapter struct {
	id        *v1beta1.ComputeTargetHTTPSProxyIdentity
	gcpClient *gcp.Client
	desired   *krm.ComputeTargetHTTPSProxy
	actual    *computepb.ComputeTargetHTTPSProxy
}

var _ directbase.Adapter = &ComputeTargetHTTPSProxyAdapter{}

func (a *ComputeTargetHTTPSProxyAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ComputeTargetHTTPSProxy", "name", a.id)

	req := &computepb.GetComputeTargetHTTPSProxyRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetComputeTargetHTTPSProxy(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ComputeTargetHTTPSProxy %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ComputeTargetHTTPSProxyAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ComputeTargetHTTPSProxy", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := ComputeTargetHTTPSProxySpec_v1beta1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &computepb.CreateComputeTargetHTTPSProxyRequest{
		Parent:                    a.id.ParentString(),
		ComputeTargetHTTPSProxyId: a.id.TargetHttpsProxy,
		ComputeTargetHTTPSProxy:   resource,
	}
	op, err := a.gcpClient.CreateComputeTargetHTTPSProxy(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ComputeTargetHTTPSProxy %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create ComputeTargetHTTPSProxy %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ComputeTargetHTTPSProxy", "name", a.id)

	status := &krm.ComputeTargetHTTPSProxyStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ComputeTargetHTTPSProxyAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ComputeTargetHTTPSProxy", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := ComputeTargetHTTPSProxySpec_v1beta1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareComputeTargetHTTPSProxy(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("ComputeTargetHTTPSProxy resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.ComputeTargetHTTPSProxyStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareComputeTargetHTTPSProxy(ctx context.Context, actual, desired *computepb.ComputeTargetHTTPSProxy) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, ComputeTargetHTTPSProxySpec_v1beta1_FromProto, ComputeTargetHTTPSProxySpec_v1beta1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *ComputeTargetHTTPSProxyAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.ComputeTargetHTTPSProxy{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(ComputeTargetHTTPSProxySpec_v1beta1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.TargetHttpsProxy)
	u.SetGroupVersionKind(krm.ComputeTargetHTTPSProxyGVK)
	return u, nil
}

func (a *ComputeTargetHTTPSProxyAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deletion not supported for ComputeTargetHTTPSProxy, removing finalizer without deleting GCP resource", "name", a.id)
	return true, nil
}
