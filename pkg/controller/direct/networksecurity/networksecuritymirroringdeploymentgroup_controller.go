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

package networksecurity

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/networksecurity/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/networksecurity/apiv1"
	pb "cloud.google.com/go/networksecurity/apiv1/networksecuritypb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.NetworkSecurityMirroringDeploymentGroupGVK, NewMirroringDeploymentGroupModel)
}

func NewMirroringDeploymentGroupModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelMirroringDeploymentGroup{config: *config}, nil
}

var _ directbase.Model = &modelMirroringDeploymentGroup{}
var _ = strings.TrimPrefix

type modelMirroringDeploymentGroup struct {
	config config.ControllerConfig
}

func (m *modelMirroringDeploymentGroup) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.NetworkSecurityMirroringDeploymentGroup{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.NetworkSecurityMirroringDeploymentGroupIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &MirroringDeploymentGroupAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelMirroringDeploymentGroup) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.NetworkSecurityMirroringDeploymentGroupIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &MirroringDeploymentGroupAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelMirroringDeploymentGroup) client(ctx context.Context) (*gcp.MirroringClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewMirroringRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building MirroringDeploymentGroup client: %w", err)
	}
	return gcpClient, err
}

type MirroringDeploymentGroupAdapter struct {
	id        *krm.NetworkSecurityMirroringDeploymentGroupIdentity
	gcpClient *gcp.MirroringClient
	desired   *krm.NetworkSecurityMirroringDeploymentGroup
	actual    *pb.MirroringDeploymentGroup
}

var _ directbase.Adapter = &MirroringDeploymentGroupAdapter{}

func (a *MirroringDeploymentGroupAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting MirroringDeploymentGroup", "name", a.id)

	req := &pb.GetMirroringDeploymentGroupRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetMirroringDeploymentGroup(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting MirroringDeploymentGroup %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *MirroringDeploymentGroupAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating MirroringDeploymentGroup", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := NetworkSecurityMirroringDeploymentGroupSpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateMirroringDeploymentGroupRequest{
		Parent:                     a.id.ParentString(),
		MirroringDeploymentGroupId: a.id.MirroringDeploymentGroup,
		MirroringDeploymentGroup:   resource,
	}
	op, err := a.gcpClient.CreateMirroringDeploymentGroup(ctx, req)
	if err != nil {
		return fmt.Errorf("creating MirroringDeploymentGroup %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create MirroringDeploymentGroup %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created MirroringDeploymentGroup", "name", a.id)

	status := &krm.NetworkSecurityMirroringDeploymentGroupStatus{}
	status.ObservedState = NetworkSecurityMirroringDeploymentGroupObservedState_v1alpha1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *MirroringDeploymentGroupAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating MirroringDeploymentGroup", "name", a.id)

	desired, diffs, updateMask, err := compareMirroringDeploymentGroup(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateMirroringDeploymentGroupRequest{
			MirroringDeploymentGroup: desired,
			UpdateMask:               updateMask,
		}
		op, err := a.gcpClient.UpdateMirroringDeploymentGroup(ctx, req)
		if err != nil {
			return fmt.Errorf("updating MirroringDeploymentGroup %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update MirroringDeploymentGroup %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated MirroringDeploymentGroup", "name", a.id)

		status := &krm.NetworkSecurityMirroringDeploymentGroupStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = NetworkSecurityMirroringDeploymentGroupObservedState_v1alpha1_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareMirroringDeploymentGroup(ctx context.Context, u *unstructured.Unstructured, actual *pb.MirroringDeploymentGroup, desiredSpec *krm.NetworkSecurityMirroringDeploymentGroupSpec) (*pb.MirroringDeploymentGroup, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		NetworkSecurityMirroringDeploymentGroupSpec_v1alpha1_FromProto,
		NetworkSecurityMirroringDeploymentGroupSpec_v1alpha1_ToProto,
		nil,
	)
}

func (a *MirroringDeploymentGroupAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.NetworkSecurityMirroringDeploymentGroup{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(NetworkSecurityMirroringDeploymentGroupSpec_v1alpha1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = &a.id.Location
	obj.Spec.ResourceID = &a.id.MirroringDeploymentGroup

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.MirroringDeploymentGroup)
	u.SetGroupVersionKind(krm.NetworkSecurityMirroringDeploymentGroupGVK)
	return u, nil
}

func (a *MirroringDeploymentGroupAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting MirroringDeploymentGroup", "name", a.id)

	req := &pb.DeleteMirroringDeploymentGroupRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteMirroringDeploymentGroup(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting MirroringDeploymentGroup %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted MirroringDeploymentGroup", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete MirroringDeploymentGroup %s: %w", a.id, err)
		}
	}
	return true, nil
}
