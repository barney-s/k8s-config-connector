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
	registry.RegisterModel(krm.NetworkSecuritySecurityProfileGroupGVK, NewSecurityProfileGroupModel)
}

func NewSecurityProfileGroupModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelSecurityProfileGroup{config: *config}, nil
}

var _ directbase.Model = &modelSecurityProfileGroup{}
var _ = strings.TrimPrefix

type modelSecurityProfileGroup struct {
	config config.ControllerConfig
}

func (m *modelSecurityProfileGroup) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.NetworkSecuritySecurityProfileGroup{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.NetworkSecuritySecurityProfileGroupIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &SecurityProfileGroupAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelSecurityProfileGroup) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelSecurityProfileGroup) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.GRPCClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building SecurityProfileGroup client: %w", err)
	}
	return gcpClient, err
}

type SecurityProfileGroupAdapter struct {
	id        *krm.NetworkSecuritySecurityProfileGroupIdentity
	gcpClient *gcp.Client
	desired   *krm.NetworkSecuritySecurityProfileGroup
	actual    *pb.SecurityProfileGroup
}

var _ directbase.Adapter = &SecurityProfileGroupAdapter{}

func (a *SecurityProfileGroupAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting SecurityProfileGroup", "name", a.id)

	req := &pb.GetSecurityProfileGroupRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetSecurityProfileGroup(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting SecurityProfileGroup %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *SecurityProfileGroupAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating SecurityProfileGroup", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := NetworkSecuritySecurityProfileGroupSpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateSecurityProfileGroupRequest{
		Parent:                 a.id.ParentString(),
		SecurityProfileGroupId: a.id.Securityprofilegroup,
		SecurityProfileGroup:   resource,
	}
	op, err := a.gcpClient.CreateSecurityProfileGroup(ctx, req)
	if err != nil {
		return fmt.Errorf("creating SecurityProfileGroup %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create SecurityProfileGroup %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created SecurityProfileGroup", "name", a.id)

	status := &krm.NetworkSecuritySecurityProfileGroupStatus{}
	status.ObservedState = NetworkSecuritySecurityProfileGroupObservedState_v1alpha1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *SecurityProfileGroupAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating SecurityProfileGroup", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := NetworkSecuritySecurityProfileGroupSpec_v1alpha1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareSecurityProfileGroup(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateSecurityProfileGroupRequest{
			SecurityProfileGroup: desired,
			UpdateMask:           updateMask,
		}
		op, err := a.gcpClient.UpdateSecurityProfileGroup(ctx, req)
		if err != nil {
			return fmt.Errorf("updating SecurityProfileGroup %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update SecurityProfileGroup %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated SecurityProfileGroup", "name", a.id)

		status := &krm.NetworkSecuritySecurityProfileGroupStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = NetworkSecuritySecurityProfileGroupObservedState_v1alpha1_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareSecurityProfileGroup(ctx context.Context, actual, desired *pb.SecurityProfileGroup) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, NetworkSecuritySecurityProfileGroupSpec_v1alpha1_FromProto, NetworkSecuritySecurityProfileGroupSpec_v1alpha1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *SecurityProfileGroupAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.NetworkSecuritySecurityProfileGroup{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(NetworkSecuritySecurityProfileGroupSpec_v1alpha1_FromProto(mapCtx, a.actual))
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
	u.SetName(a.id.Securityprofilegroup)
	u.SetGroupVersionKind(krm.NetworkSecuritySecurityProfileGroupGVK)
	return u, nil
}

func (a *SecurityProfileGroupAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting SecurityProfileGroup", "name", a.id)

	req := &pb.DeleteSecurityProfileGroupRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteSecurityProfileGroup(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting SecurityProfileGroup %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted SecurityProfileGroup", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete SecurityProfileGroup %s: %w", a.id, err)
		}
	}
	return true, nil
}
