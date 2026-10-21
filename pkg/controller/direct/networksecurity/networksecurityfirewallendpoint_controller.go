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
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
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
	registry.RegisterModel(krm.NetworkSecurityFirewallEndpointGVK, NewProjectFirewallEndpointModel)
}

func NewProjectFirewallEndpointModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelProjectFirewallEndpoint{config: *config}, nil
}

var _ directbase.Model = &modelProjectFirewallEndpoint{}
var _ = strings.TrimPrefix

type modelProjectFirewallEndpoint struct {
	config config.ControllerConfig
}

func (m *modelProjectFirewallEndpoint) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.NetworkSecurityFirewallEndpoint{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.NetworkSecurityFirewallEndpointIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ProjectFirewallEndpointAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelProjectFirewallEndpoint) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelProjectFirewallEndpoint) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ProjectFirewallEndpoint client: %w", err)
	}
	return gcpClient, err
}

type ProjectFirewallEndpointAdapter struct {
	id        *krm.NetworkSecurityFirewallEndpointIdentity
	gcpClient *gcp.Client
	desired   *krm.NetworkSecurityFirewallEndpoint
	actual    *pb.ProjectFirewallEndpoint
}

var _ directbase.Adapter = &ProjectFirewallEndpointAdapter{}

func (a *ProjectFirewallEndpointAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ProjectFirewallEndpoint", "name", a.id)

	req := &pb.GetProjectFirewallEndpointRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetProjectFirewallEndpoint(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ProjectFirewallEndpoint %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ProjectFirewallEndpointAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ProjectFirewallEndpoint", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := NetworkSecurityFirewallEndpointSpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateProjectFirewallEndpointRequest{
		Parent:                  a.id.ParentString(),
		FirewallEndpointId:      a.id.FirewallEndpoint,
		ProjectFirewallEndpoint: resource,
	}
	op, err := a.gcpClient.CreateProjectFirewallEndpoint(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ProjectFirewallEndpoint %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create ProjectFirewallEndpoint %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ProjectFirewallEndpoint", "name", a.id)

	status := &krm.NetworkSecurityFirewallEndpointStatus{}
	status.ObservedState = NetworkSecurityFirewallEndpointObservedState_v1alpha1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ProjectFirewallEndpointAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ProjectFirewallEndpoint", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := NetworkSecurityFirewallEndpointSpec_v1alpha1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareProjectFirewallEndpoint(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateProjectFirewallEndpointRequest{
			ProjectFirewallEndpoint: desired,
			UpdateMask:              updateMask,
		}
		op, err := a.gcpClient.UpdateProjectFirewallEndpoint(ctx, req)
		if err != nil {
			return fmt.Errorf("updating ProjectFirewallEndpoint %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update ProjectFirewallEndpoint %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated ProjectFirewallEndpoint", "name", a.id)

		status := &krm.NetworkSecurityFirewallEndpointStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = NetworkSecurityFirewallEndpointObservedState_v1alpha1_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareProjectFirewallEndpoint(ctx context.Context, actual, desired *pb.ProjectFirewallEndpoint) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, NetworkSecurityFirewallEndpointSpec_v1alpha1_FromProto, NetworkSecurityFirewallEndpointSpec_v1alpha1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *ProjectFirewallEndpointAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.NetworkSecurityFirewallEndpoint{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(NetworkSecurityFirewallEndpointSpec_v1alpha1_FromProto(mapCtx, a.actual))
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
	u.SetName(a.id.FirewallEndpoint)
	u.SetGroupVersionKind(krm.NetworkSecurityFirewallEndpointGVK)
	return u, nil
}

func (a *ProjectFirewallEndpointAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting ProjectFirewallEndpoint", "name", a.id)

	req := &pb.DeleteProjectFirewallEndpointRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteProjectFirewallEndpoint(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting ProjectFirewallEndpoint %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted ProjectFirewallEndpoint", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete ProjectFirewallEndpoint %s: %w", a.id, err)
		}
	}
	return true, nil
}
