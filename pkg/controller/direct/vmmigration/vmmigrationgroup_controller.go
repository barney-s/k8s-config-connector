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

package vmmigration

import (
	"context"
	"fmt"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/vmmigration/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/vmmigration/apiv1"
	pb "cloud.google.com/go/vmmigration/apiv1/vmmigrationpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.VMMigrationGroupGVK, NewGroupModel)
}

func NewGroupModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelGroup{config: *config}, nil
}

var _ directbase.Model = &modelGroup{}
var _ = strings.TrimPrefix

type modelGroup struct {
	config config.ControllerConfig
}

func (m *modelGroup) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.VMMigrationGroup{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.VMMigrationGroupIdentity)

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &GroupAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelGroup) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.VMMigrationGroupIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &GroupAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelGroup) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Group client: %w", err)
	}
	return gcpClient, err
}

type GroupAdapter struct {
	id        *krm.VMMigrationGroupIdentity
	gcpClient *gcp.Client
	desired   *krm.VMMigrationGroup
	actual    *pb.Group
}

var _ directbase.Adapter = &GroupAdapter{}

func (a *GroupAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Group", "name", a.id)

	req := &pb.GetGroupRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetGroup(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Group %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *GroupAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Group", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := VMMigrationGroupSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateGroupRequest{
		Parent:  a.id.ParentString(),
		GroupId: a.id.Group,
		Group:   resource,
	}
	op, err := a.gcpClient.CreateGroup(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Group %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create Group %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Group", "name", a.id)

	status := &krm.VMMigrationGroupStatus{}
	status.ObservedState = VMMigrationGroupObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *GroupAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Group", "name", a.id)

	desired, diffs, updateMask, err := compareGroup(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateGroupRequest{
			Group:      desired,
			UpdateMask: updateMask,
		}
		op, err := a.gcpClient.UpdateGroup(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Group %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update Group %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Group", "name", a.id)

		status := &krm.VMMigrationGroupStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = VMMigrationGroupObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareGroup(ctx context.Context, u *unstructured.Unstructured, actual *pb.Group, desiredSpec *krm.VMMigrationGroupSpec) (*pb.Group, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		VMMigrationGroupSpec_FromProto,
		VMMigrationGroupSpec_ToProto,
		nil,
	)
}

func (a *GroupAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.VMMigrationGroup{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(VMMigrationGroupSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = &a.id.Location
	obj.Spec.ResourceID = &a.id.Group

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Group)
	u.SetGroupVersionKind(krm.VMMigrationGroupGVK)
	return u, nil
}

func (a *GroupAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Group", "name", a.id)

	req := &pb.DeleteGroupRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteGroup(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Group %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Group", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete Group %s: %w", a.id, err)
		}
	}
	return true, nil
}
