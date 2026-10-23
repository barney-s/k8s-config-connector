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
	registry.RegisterModel(krm.VMwareEngineNetworkPeeringGVK, NewNetworkPeeringModel)
}

func NewNetworkPeeringModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelNetworkPeering{config: *config}, nil
}

var _ directbase.Model = &modelNetworkPeering{}
var _ = strings.TrimPrefix

type modelNetworkPeering struct {
	config config.ControllerConfig
}

func (m *modelNetworkPeering) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.VMwareEngineNetworkPeering{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id, err := krm.NewNetworkPeeringIdentity(ctx, reader, obj)
	if err != nil {
		return nil, err
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &NetworkPeeringAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelNetworkPeering) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelNetworkPeering) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building NetworkPeering client: %w", err)
	}
	return gcpClient, err
}

type NetworkPeeringAdapter struct {
	id        *krm.NetworkPeeringIdentity
	gcpClient *gcp.Client
	desired   *krm.VMwareEngineNetworkPeering
	actual    *pb.NetworkPeering
}

var _ directbase.Adapter = &NetworkPeeringAdapter{}

func (a *NetworkPeeringAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting NetworkPeering", "name", a.id)

	req := &pb.GetNetworkPeeringRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetNetworkPeering(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting NetworkPeering %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *NetworkPeeringAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating NetworkPeering", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := VMwareEngineNetworkPeeringSpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateNetworkPeeringRequest{
		Parent:           a.id.Parent().String(),
		NetworkPeeringId: a.id.ID(),
		NetworkPeering:   resource,
	}
	op, err := a.gcpClient.CreateNetworkPeering(ctx, req)
	if err != nil {
		return fmt.Errorf("creating NetworkPeering %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create NetworkPeering %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created NetworkPeering", "name", a.id)

	status := &krm.VMwareEngineNetworkPeeringStatus{}
	status.ObservedState = VMwareEngineNetworkPeeringObservedState_v1alpha1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *NetworkPeeringAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating NetworkPeering", "name", a.id)

	desired, diffs, updateMask, err := compareNetworkPeering(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateNetworkPeeringRequest{
			NetworkPeering: desired,
			UpdateMask:     updateMask,
		}
		op, err := a.gcpClient.UpdateNetworkPeering(ctx, req)
		if err != nil {
			return fmt.Errorf("updating NetworkPeering %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update NetworkPeering %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated NetworkPeering", "name", a.id)

		status := &krm.VMwareEngineNetworkPeeringStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = VMwareEngineNetworkPeeringObservedState_v1alpha1_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareNetworkPeering(ctx context.Context, u *unstructured.Unstructured, actual *pb.NetworkPeering, desiredSpec *krm.VMwareEngineNetworkPeeringSpec) (*pb.NetworkPeering, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		VMwareEngineNetworkPeeringSpec_v1alpha1_FromProto,
		VMwareEngineNetworkPeeringSpec_v1alpha1_ToProto,
		nil,
	)
}

func (a *NetworkPeeringAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.VMwareEngineNetworkPeering{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(VMwareEngineNetworkPeeringSpec_v1alpha1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Parent().ProjectID}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.VMwareEngineNetworkPeeringGVK)
	return u, nil
}

func (a *NetworkPeeringAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting NetworkPeering", "name", a.id)

	req := &pb.DeleteNetworkPeeringRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteNetworkPeering(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting NetworkPeering %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted NetworkPeering", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete NetworkPeering %s: %w", a.id, err)
		}
	}
	return true, nil
}
