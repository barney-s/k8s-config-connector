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
	registry.RegisterModel(krm.VMwareEngineNetworkPolicyGVK, NewNetworkPolicyModel)
}

func NewNetworkPolicyModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelNetworkPolicy{config: *config}, nil
}

var _ directbase.Model = &modelNetworkPolicy{}
var _ = strings.TrimPrefix

type modelNetworkPolicy struct {
	config config.ControllerConfig
}

func (m *modelNetworkPolicy) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.VMwareEngineNetworkPolicy{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.NetworkPolicyIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &NetworkPolicyAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelNetworkPolicy) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelNetworkPolicy) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building NetworkPolicy client: %w", err)
	}
	return gcpClient, err
}

type NetworkPolicyAdapter struct {
	id        *krm.NetworkPolicyIdentity
	gcpClient *gcp.Client
	desired   *krm.VMwareEngineNetworkPolicy
	actual    *pb.NetworkPolicy
}

var _ directbase.Adapter = &NetworkPolicyAdapter{}

func (a *NetworkPolicyAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting NetworkPolicy", "name", a.id)

	req := &pb.GetNetworkPolicyRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetNetworkPolicy(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting NetworkPolicy %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *NetworkPolicyAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating NetworkPolicy", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := VMwareEngineNetworkPolicySpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateNetworkPolicyRequest{
		Parent:          a.id.Parent().String(),
		NetworkPolicyId: a.id.ID(),
		NetworkPolicy:   resource,
	}
	op, err := a.gcpClient.CreateNetworkPolicy(ctx, req)
	if err != nil {
		return fmt.Errorf("creating NetworkPolicy %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create NetworkPolicy %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created NetworkPolicy", "name", a.id)

	status := &krm.VMwareEngineNetworkPolicyStatus{}
	status.ObservedState = VMwareEngineNetworkPolicyObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *NetworkPolicyAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating NetworkPolicy", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := VMwareEngineNetworkPolicySpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareNetworkPolicy(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateNetworkPolicyRequest{
			NetworkPolicy: desired,
			UpdateMask:    updateMask,
		}
		op, err := a.gcpClient.UpdateNetworkPolicy(ctx, req)
		if err != nil {
			return fmt.Errorf("updating NetworkPolicy %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update NetworkPolicy %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated NetworkPolicy", "name", a.id)

		status := &krm.VMwareEngineNetworkPolicyStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = VMwareEngineNetworkPolicyObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareNetworkPolicy(ctx context.Context, actual, desired *pb.NetworkPolicy) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, VMwareEngineNetworkPolicySpec_FromProto, VMwareEngineNetworkPolicySpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *NetworkPolicyAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.VMwareEngineNetworkPolicy{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(VMwareEngineNetworkPolicySpec_FromProto(mapCtx, a.actual))
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
	u.SetGroupVersionKind(krm.VMwareEngineNetworkPolicyGVK)
	return u, nil
}

func (a *NetworkPolicyAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting NetworkPolicy", "name", a.id)

	req := &pb.DeleteNetworkPolicyRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteNetworkPolicy(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting NetworkPolicy %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted NetworkPolicy", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete NetworkPolicy %s: %w", a.id, err)
		}
	}
	return true, nil
}
