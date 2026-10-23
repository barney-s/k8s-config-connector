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

package interceptendpointgroup

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
	registry.RegisterModel(krm.NetworkSecurityInterceptEndpointGroupGVK, NewInterceptEndpointGroupModel)
}

func NewInterceptEndpointGroupModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelInterceptEndpointGroup{config: *config}, nil
}

var _ directbase.Model = &modelInterceptEndpointGroup{}
var _ = strings.TrimPrefix

type modelInterceptEndpointGroup struct {
	config config.ControllerConfig
}

func (m *modelInterceptEndpointGroup) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.NetworkSecurityInterceptEndpointGroup{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.NetworkSecurityInterceptEndpointGroupIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &InterceptEndpointGroupAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelInterceptEndpointGroup) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelInterceptEndpointGroup) client(ctx context.Context) (*gcp.InterceptClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.GRPCClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewInterceptClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building InterceptEndpointGroup client: %w", err)
	}
	return gcpClient, err
}

type InterceptEndpointGroupAdapter struct {
	id        *krm.NetworkSecurityInterceptEndpointGroupIdentity
	gcpClient *gcp.InterceptClient
	desired   *krm.NetworkSecurityInterceptEndpointGroup
	actual    *pb.InterceptEndpointGroup
}

var _ directbase.Adapter = &InterceptEndpointGroupAdapter{}

func (a *InterceptEndpointGroupAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting InterceptEndpointGroup", "name", a.id)

	req := &pb.GetInterceptEndpointGroupRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetInterceptEndpointGroup(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting InterceptEndpointGroup %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *InterceptEndpointGroupAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating InterceptEndpointGroup", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := NetworkSecurityInterceptEndpointGroupSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateInterceptEndpointGroupRequest{
		Parent:                   a.id.ParentString(),
		InterceptEndpointGroupId: a.id.Interceptendpointgroup,
		InterceptEndpointGroup:   resource,
	}
	op, err := a.gcpClient.CreateInterceptEndpointGroup(ctx, req)
	if err != nil {
		return fmt.Errorf("creating InterceptEndpointGroup %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create InterceptEndpointGroup %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created InterceptEndpointGroup", "name", a.id)

	status := &krm.NetworkSecurityInterceptEndpointGroupStatus{}
	status.ObservedState = NetworkSecurityInterceptEndpointGroupObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *InterceptEndpointGroupAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating InterceptEndpointGroup", "name", a.id)

	desired, diffs, updateMask, err := compareInterceptEndpointGroup(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateInterceptEndpointGroupRequest{
			InterceptEndpointGroup: desired,
			UpdateMask:             updateMask,
		}
		op, err := a.gcpClient.UpdateInterceptEndpointGroup(ctx, req)
		if err != nil {
			return fmt.Errorf("updating InterceptEndpointGroup %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update InterceptEndpointGroup %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated InterceptEndpointGroup", "name", a.id)

		status := &krm.NetworkSecurityInterceptEndpointGroupStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = NetworkSecurityInterceptEndpointGroupObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareInterceptEndpointGroup(ctx context.Context, u *unstructured.Unstructured, actual *pb.InterceptEndpointGroup, desiredSpec *krm.NetworkSecurityInterceptEndpointGroupSpec) (*pb.InterceptEndpointGroup, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		NetworkSecurityInterceptEndpointGroupSpec_FromProto,
		NetworkSecurityInterceptEndpointGroupSpec_ToProto,
		nil,
	)
}

func (a *InterceptEndpointGroupAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.NetworkSecurityInterceptEndpointGroup{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(NetworkSecurityInterceptEndpointGroupSpec_FromProto(mapCtx, a.actual))
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
	u.SetName(a.id.Interceptendpointgroup)
	u.SetGroupVersionKind(krm.NetworkSecurityInterceptEndpointGroupGVK)
	return u, nil
}

func (a *InterceptEndpointGroupAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting InterceptEndpointGroup", "name", a.id)

	req := &pb.DeleteInterceptEndpointGroupRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteInterceptEndpointGroup(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting InterceptEndpointGroup %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted InterceptEndpointGroup", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete InterceptEndpointGroup %s: %w", a.id, err)
		}
	}
	return true, nil
}
