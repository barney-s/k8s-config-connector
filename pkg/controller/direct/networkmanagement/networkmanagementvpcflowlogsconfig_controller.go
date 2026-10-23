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

package networkmanagement

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/networkmanagement/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/networkmanagement/apiv1"
	pb "cloud.google.com/go/networkmanagement/apiv1/networkmanagementpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.NetworkManagementVPCFlowLogsConfigGVK, NewVpcFlowLogsConfigModel)
}

func NewVpcFlowLogsConfigModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelVpcFlowLogsConfig{config: *config}, nil
}

var _ directbase.Model = &modelVpcFlowLogsConfig{}
var _ = strings.TrimPrefix

type modelVpcFlowLogsConfig struct {
	config config.ControllerConfig
}

func (m *modelVpcFlowLogsConfig) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.NetworkManagementVPCFlowLogsConfig{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id, err := krm.NewVpcFlowLogsConfigIdentity(ctx, reader, obj)
	if err != nil {
		return nil, err
	}
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &VpcFlowLogsConfigAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelVpcFlowLogsConfig) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelVpcFlowLogsConfig) client(ctx context.Context) (*gcp.VpcFlowLogsClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewVpcFlowLogsRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building VpcFlowLogsConfig client: %w", err)
	}
	return gcpClient, err
}

type VpcFlowLogsConfigAdapter struct {
	id        *krm.VpcFlowLogsConfigIdentity
	gcpClient *gcp.VpcFlowLogsClient
	desired   *krm.NetworkManagementVPCFlowLogsConfig
	actual    *pb.VpcFlowLogsConfig
}

var _ directbase.Adapter = &VpcFlowLogsConfigAdapter{}

func (a *VpcFlowLogsConfigAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting VpcFlowLogsConfig", "name", a.id)

	req := &pb.GetVpcFlowLogsConfigRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetVpcFlowLogsConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting VpcFlowLogsConfig %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *VpcFlowLogsConfigAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating VpcFlowLogsConfig", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := NetworkManagementVPCFlowLogsConfigSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateVpcFlowLogsConfigRequest{
		Parent:              a.id.Parent().String(),
		VpcFlowLogsConfigId: a.id.ID(),
		VpcFlowLogsConfig:   resource,
	}
	op, err := a.gcpClient.CreateVpcFlowLogsConfig(ctx, req)
	if err != nil {
		return fmt.Errorf("creating VpcFlowLogsConfig %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create VpcFlowLogsConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created VpcFlowLogsConfig", "name", a.id)

	status := &krm.NetworkManagementVPCFlowLogsConfigStatus{}
	status.ObservedState = NetworkManagementVPCFlowLogsConfigObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *VpcFlowLogsConfigAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating VpcFlowLogsConfig", "name", a.id)

	desired, diffs, updateMask, err := compareVpcFlowLogsConfig(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateVpcFlowLogsConfigRequest{
			VpcFlowLogsConfig: desired,
			UpdateMask:        updateMask,
		}
		op, err := a.gcpClient.UpdateVpcFlowLogsConfig(ctx, req)
		if err != nil {
			return fmt.Errorf("updating VpcFlowLogsConfig %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update VpcFlowLogsConfig %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated VpcFlowLogsConfig", "name", a.id)

		status := &krm.NetworkManagementVPCFlowLogsConfigStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = NetworkManagementVPCFlowLogsConfigObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareVpcFlowLogsConfig(ctx context.Context, u *unstructured.Unstructured, actual *pb.VpcFlowLogsConfig, desiredSpec *krm.NetworkManagementVPCFlowLogsConfigSpec) (*pb.VpcFlowLogsConfig, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		NetworkManagementVPCFlowLogsConfigSpec_FromProto,
		NetworkManagementVPCFlowLogsConfigSpec_ToProto,
		nil,
	)
}

func (a *VpcFlowLogsConfigAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.NetworkManagementVPCFlowLogsConfig{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(NetworkManagementVPCFlowLogsConfigSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Parent().ProjectID}
	obj.Spec.Location = direct.PtrTo(a.id.Parent().Location)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.NetworkManagementVPCFlowLogsConfigGVK)
	return u, nil
}

func (a *VpcFlowLogsConfigAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting VpcFlowLogsConfig", "name", a.id)

	req := &pb.DeleteVpcFlowLogsConfigRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteVpcFlowLogsConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting VpcFlowLogsConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted VpcFlowLogsConfig", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete VpcFlowLogsConfig %s: %w", a.id, err)
		}
	}
	return true, nil
}
