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
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/export"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/label"
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
	registry.RegisterModel(krm.ComputeExternalVPNGatewayGVK, NewComputeExternalVPNGatewayModel)
}

func NewComputeExternalVPNGatewayModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelComputeExternalVPNGateway{config: *config}, nil
}

var _ directbase.Model = &modelComputeExternalVPNGateway{}
var _ = strings.TrimPrefix

type modelComputeExternalVPNGateway struct {
	config config.ControllerConfig
}

func (m *modelComputeExternalVPNGateway) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.ComputeExternalVPNGateway{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.ComputeExternalVPNGatewayIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ComputeExternalVPNGatewayAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelComputeExternalVPNGateway) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.ComputeExternalVPNGatewayIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ComputeExternalVPNGatewayAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelComputeExternalVPNGateway) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ComputeExternalVPNGateway client: %w", err)
	}
	return gcpClient, err
}

type ComputeExternalVPNGatewayAdapter struct {
	id        *krm.ComputeExternalVPNGatewayIdentity
	gcpClient *gcp.Client
	desired   *krm.ComputeExternalVPNGateway
	actual    *pb.ComputeExternalVPNGateway
}

var _ directbase.Adapter = &ComputeExternalVPNGatewayAdapter{}

func (a *ComputeExternalVPNGatewayAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ComputeExternalVPNGateway", "name", a.id)

	req := &pb.GetComputeExternalVPNGatewayRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetComputeExternalVPNGateway(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ComputeExternalVPNGateway %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ComputeExternalVPNGatewayAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ComputeExternalVPNGateway", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := ComputeExternalVPNGatewaySpec_v1beta1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateComputeExternalVPNGatewayRequest{
		Parent:                      a.id.ParentString(),
		ComputeExternalVPNGatewayId: a.id.ExternalVPNGateway,
		ComputeExternalVPNGateway:   resource,
	}
	op, err := a.gcpClient.CreateComputeExternalVPNGateway(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ComputeExternalVPNGateway %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create ComputeExternalVPNGateway %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ComputeExternalVPNGateway", "name", a.id)

	status := &krm.ComputeExternalVPNGatewayStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ComputeExternalVPNGatewayAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ComputeExternalVPNGateway", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := ComputeExternalVPNGatewaySpec_v1beta1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareComputeExternalVPNGateway(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("ComputeExternalVPNGateway resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.ComputeExternalVPNGatewayStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareComputeExternalVPNGateway(ctx context.Context, actual, desired *pb.ComputeExternalVPNGateway) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, ComputeExternalVPNGatewaySpec_v1beta1_FromProto, ComputeExternalVPNGatewaySpec_v1beta1_ToProto)
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

func (a *ComputeExternalVPNGatewayAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.ComputeExternalVPNGateway{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(ComputeExternalVPNGatewaySpec_v1beta1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ExternalVPNGateway)
	u.SetGroupVersionKind(krm.ComputeExternalVPNGatewayGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *ComputeExternalVPNGatewayAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deletion not supported for ComputeExternalVPNGateway, removing finalizer without deleting GCP resource", "name", a.id)
	return true, nil
}
