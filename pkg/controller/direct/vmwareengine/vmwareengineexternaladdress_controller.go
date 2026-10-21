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

package vmwareengine

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/vmwareengine/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/vmwareengine/apiv1"
	pb "cloud.google.com/go/vmwareengine/apiv1/vmwareenginepb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.VMwareEngineExternalAddressGVK, NewExternalAddressModel)
}

func NewExternalAddressModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelExternalAddress{config: *config}, nil
}

var _ directbase.Model = &modelExternalAddress{}
var _ = strings.TrimPrefix

type modelExternalAddress struct {
	config config.ControllerConfig
}

func (m *modelExternalAddress) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.VMwareEngineExternalAddress{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.ExternalAddressIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ExternalAddressAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelExternalAddress) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelExternalAddress) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ExternalAddress client: %w", err)
	}
	return gcpClient, err
}

type ExternalAddressAdapter struct {
	id        *krm.ExternalAddressIdentity
	gcpClient *gcp.Client
	desired   *krm.VMwareEngineExternalAddress
	actual    *pb.ExternalAddress
}

var _ directbase.Adapter = &ExternalAddressAdapter{}

func (a *ExternalAddressAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ExternalAddress", "name", a.id)

	req := &pb.GetExternalAddressRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetExternalAddress(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ExternalAddress %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ExternalAddressAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ExternalAddress", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := VMwareEngineExternalAddressSpec_v1beta1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateExternalAddressRequest{
		Parent:            a.id.Parent().String(),
		ExternalAddressId: a.id.ID(),
		ExternalAddress:   resource,
	}
	op, err := a.gcpClient.CreateExternalAddress(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ExternalAddress %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create ExternalAddress %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ExternalAddress", "name", a.id)

	status := &krm.VMwareEngineExternalAddressStatus{}
	status.ObservedState = VMwareEngineExternalAddressObservedState_v1beta1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ExternalAddressAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ExternalAddress", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := VMwareEngineExternalAddressSpec_v1beta1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareExternalAddress(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateExternalAddressRequest{
			ExternalAddress: desired,
			UpdateMask:      updateMask,
		}
		op, err := a.gcpClient.UpdateExternalAddress(ctx, req)
		if err != nil {
			return fmt.Errorf("updating ExternalAddress %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update ExternalAddress %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated ExternalAddress", "name", a.id)

		status := &krm.VMwareEngineExternalAddressStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = VMwareEngineExternalAddressObservedState_v1beta1_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareExternalAddress(ctx context.Context, actual, desired *pb.ExternalAddress) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, VMwareEngineExternalAddressSpec_v1beta1_FromProto, VMwareEngineExternalAddressSpec_v1beta1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *ExternalAddressAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.VMwareEngineExternalAddress{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(VMwareEngineExternalAddressSpec_v1beta1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.VMwareEngineExternalAddressGVK)
	return u, nil
}

func (a *ExternalAddressAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting ExternalAddress", "name", a.id)

	req := &pb.DeleteExternalAddressRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteExternalAddress(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting ExternalAddress %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted ExternalAddress", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete ExternalAddress %s: %w", a.id, err)
		}
	}
	return true, nil
}
