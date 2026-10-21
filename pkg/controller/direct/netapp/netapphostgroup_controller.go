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

package netapp

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/netapp/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/netapp/apiv1"
	netapppb "cloud.google.com/go/netapp/apiv1/netapppb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.NetAppHostGroupGVK, NewHostGroupModel)
}

func NewHostGroupModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelHostGroup{config: *config}, nil
}

var _ directbase.Model = &modelHostGroup{}
var _ = strings.TrimPrefix

type modelHostGroup struct {
	config config.ControllerConfig
}

func (m *modelHostGroup) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.NetAppHostGroup{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.NetAppHostGroupIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &HostGroupAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelHostGroup) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.NetAppHostGroupIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &HostGroupAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelHostGroup) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building HostGroup client: %w", err)
	}
	return gcpClient, err
}

type HostGroupAdapter struct {
	id        *krm.NetAppHostGroupIdentity
	gcpClient *gcp.Client
	desired   *krm.NetAppHostGroup
	actual    *netapppb.HostGroup
}

var _ directbase.Adapter = &HostGroupAdapter{}

func (a *HostGroupAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting HostGroup", "name", a.id)

	req := &netapppb.GetHostGroupRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetHostGroup(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting HostGroup %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *HostGroupAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating HostGroup", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := NetAppHostGroupSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &netapppb.CreateHostGroupRequest{
		Parent:      a.id.ParentString(),
		HostGroupId: a.id.HostGroup,
		HostGroup:   resource,
	}
	op, err := a.gcpClient.CreateHostGroup(ctx, req)
	if err != nil {
		return fmt.Errorf("creating HostGroup %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create HostGroup %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created HostGroup", "name", a.id)

	status := &krm.NetAppHostGroupStatus{}
	status.ObservedState = NetAppHostGroupObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *HostGroupAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating HostGroup", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := NetAppHostGroupSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareHostGroup(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &netapppb.UpdateHostGroupRequest{
			HostGroup:  desired,
			UpdateMask: updateMask,
		}
		op, err := a.gcpClient.UpdateHostGroup(ctx, req)
		if err != nil {
			return fmt.Errorf("updating HostGroup %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update HostGroup %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated HostGroup", "name", a.id)

		status := &krm.NetAppHostGroupStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = NetAppHostGroupObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareHostGroup(ctx context.Context, actual, desired *netapppb.HostGroup) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, NetAppHostGroupSpec_FromProto, NetAppHostGroupSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *HostGroupAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.NetAppHostGroup{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(NetAppHostGroupSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = a.id.Location
	obj.Spec.ResourceID = direct.LazyPtr(a.id.HostGroup)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.HostGroup)
	u.SetGroupVersionKind(krm.NetAppHostGroupGVK)
	return u, nil
}

func (a *HostGroupAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting HostGroup", "name", a.id)

	req := &netapppb.DeleteHostGroupRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteHostGroup(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting HostGroup %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted HostGroup", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete HostGroup %s: %w", a.id, err)
		}
	}
	return true, nil
}
