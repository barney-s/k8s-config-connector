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

package apphub

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/apphub/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/apphub/apiv1"
	apphubpb "cloud.google.com/go/apphub/apiv1/apphubpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.AppHubServiceProjectAttachmentGVK, NewServiceProjectAttachmentModel)
}

func NewServiceProjectAttachmentModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelServiceProjectAttachment{config: *config}, nil
}

var _ directbase.Model = &modelServiceProjectAttachment{}
var _ = strings.TrimPrefix

type modelServiceProjectAttachment struct {
	config config.ControllerConfig
}

func (m *modelServiceProjectAttachment) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.AppHubServiceProjectAttachment{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.AppHubServiceProjectAttachmentIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ServiceProjectAttachmentAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelServiceProjectAttachment) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelServiceProjectAttachment) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ServiceProjectAttachment client: %w", err)
	}
	return gcpClient, err
}

type ServiceProjectAttachmentAdapter struct {
	id        *krm.AppHubServiceProjectAttachmentIdentity
	gcpClient *gcp.Client
	desired   *krm.AppHubServiceProjectAttachment
	actual    *apphubpb.ServiceProjectAttachment
}

var _ directbase.Adapter = &ServiceProjectAttachmentAdapter{}

func (a *ServiceProjectAttachmentAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ServiceProjectAttachment", "name", a.id)

	req := &apphubpb.GetServiceProjectAttachmentRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetServiceProjectAttachment(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ServiceProjectAttachment %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ServiceProjectAttachmentAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ServiceProjectAttachment", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := AppHubServiceProjectAttachmentSpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &apphubpb.CreateServiceProjectAttachmentRequest{
		Parent:                     a.id.ParentString(),
		ServiceProjectAttachmentId: a.id.ServiceProjectAttachment,
		ServiceProjectAttachment:   resource,
	}
	op, err := a.gcpClient.CreateServiceProjectAttachment(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ServiceProjectAttachment %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create ServiceProjectAttachment %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ServiceProjectAttachment", "name", a.id)

	status := &krm.AppHubServiceProjectAttachmentStatus{}
	status.ObservedState = AppHubServiceProjectAttachmentObservedState_v1alpha1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ServiceProjectAttachmentAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ServiceProjectAttachment", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := AppHubServiceProjectAttachmentSpec_v1alpha1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareServiceProjectAttachment(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("AppHubServiceProjectAttachment resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.AppHubServiceProjectAttachmentStatus{}
	mapCtx = &direct.MapContext{}
	status.ObservedState = AppHubServiceProjectAttachmentObservedState_v1alpha1_FromProto(mapCtx, a.actual)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareServiceProjectAttachment(ctx context.Context, actual, desired *apphubpb.ServiceProjectAttachment) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, AppHubServiceProjectAttachmentSpec_v1alpha1_FromProto, AppHubServiceProjectAttachmentSpec_v1alpha1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *ServiceProjectAttachmentAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.AppHubServiceProjectAttachment{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(AppHubServiceProjectAttachmentSpec_v1alpha1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = a.id.Location
	obj.Spec.ResourceID = direct.LazyPtr(a.id.ServiceProjectAttachment)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ServiceProjectAttachment)
	u.SetGroupVersionKind(krm.AppHubServiceProjectAttachmentGVK)
	return u, nil
}

func (a *ServiceProjectAttachmentAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting ServiceProjectAttachment", "name", a.id)

	req := &apphubpb.DeleteServiceProjectAttachmentRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteServiceProjectAttachment(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting ServiceProjectAttachment %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted ServiceProjectAttachment", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete ServiceProjectAttachment %s: %w", a.id, err)
		}
	}
	return true, nil
}
