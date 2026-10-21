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
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/vmwareengine/v1alpha1"
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
	registry.RegisterModel(krm.VMwareEngineNetworkGVK, NewVmwareEngineNetworkModel)
}

func NewVmwareEngineNetworkModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelVmwareEngineNetwork{config: *config}, nil
}

var _ directbase.Model = &modelVmwareEngineNetwork{}
var _ = strings.TrimPrefix

type modelVmwareEngineNetwork struct {
	config config.ControllerConfig
}

func (m *modelVmwareEngineNetwork) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.VMwareEngineNetwork{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.VmwareEngineNetworkIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &VmwareEngineNetworkAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelVmwareEngineNetwork) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelVmwareEngineNetwork) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building VmwareEngineNetwork client: %w", err)
	}
	return gcpClient, err
}

type VmwareEngineNetworkAdapter struct {
	id        *krm.VmwareEngineNetworkIdentity
	gcpClient *gcp.Client
	desired   *krm.VMwareEngineNetwork
	actual    *pb.VmwareEngineNetwork
}

var _ directbase.Adapter = &VmwareEngineNetworkAdapter{}

func (a *VmwareEngineNetworkAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting VmwareEngineNetwork", "name", a.id)

	req := &pb.GetVmwareEngineNetworkRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetVmwareEngineNetwork(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting VmwareEngineNetwork %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *VmwareEngineNetworkAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating VmwareEngineNetwork", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := VMwareEngineNetworkSpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateVmwareEngineNetworkRequest{
		Parent:                a.id.Parent().String(),
		VmwareEngineNetworkId: a.id.ID(),
		VmwareEngineNetwork:   resource,
	}
	op, err := a.gcpClient.CreateVmwareEngineNetwork(ctx, req)
	if err != nil {
		return fmt.Errorf("creating VmwareEngineNetwork %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create VmwareEngineNetwork %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created VmwareEngineNetwork", "name", a.id)

	status := &krm.VMwareEngineNetworkStatus{}
	status.ObservedState = VMwareEngineNetworkObservedState_v1alpha1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *VmwareEngineNetworkAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating VmwareEngineNetwork", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := VMwareEngineNetworkSpec_v1alpha1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareVmwareEngineNetwork(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateVmwareEngineNetworkRequest{
			VmwareEngineNetwork: desired,
			UpdateMask:          updateMask,
		}
		op, err := a.gcpClient.UpdateVmwareEngineNetwork(ctx, req)
		if err != nil {
			return fmt.Errorf("updating VmwareEngineNetwork %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update VmwareEngineNetwork %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated VmwareEngineNetwork", "name", a.id)

		status := &krm.VMwareEngineNetworkStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = VMwareEngineNetworkObservedState_v1alpha1_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareVmwareEngineNetwork(ctx context.Context, actual, desired *pb.VmwareEngineNetwork) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, VMwareEngineNetworkSpec_v1alpha1_FromProto, VMwareEngineNetworkSpec_v1alpha1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *VmwareEngineNetworkAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.VMwareEngineNetwork{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(VMwareEngineNetworkSpec_v1alpha1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = a.id.Location

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.VMwareEngineNetworkGVK)
	return u, nil
}

func (a *VmwareEngineNetworkAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting VmwareEngineNetwork", "name", a.id)

	req := &pb.DeleteVmwareEngineNetworkRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteVmwareEngineNetwork(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting VmwareEngineNetwork %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted VmwareEngineNetwork", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete VmwareEngineNetwork %s: %w", a.id, err)
		}
	}
	return true, nil
}
