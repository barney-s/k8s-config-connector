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

package apihubruntimeprojectattachment

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/apihub/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/apihub/apiv1"
	pb "cloud.google.com/go/apihub/apiv1/apihubpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.APIHubRuntimeProjectAttachmentGVK, NewRuntimeProjectAttachmentModel)
}

func NewRuntimeProjectAttachmentModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelRuntimeProjectAttachment{config: *config}, nil
}

var _ directbase.Model = &modelRuntimeProjectAttachment{}
var _ = strings.TrimPrefix

type modelRuntimeProjectAttachment struct {
	config config.ControllerConfig
}

func (m *modelRuntimeProjectAttachment) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.APIHubRuntimeProjectAttachment{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.APIHubRuntimeProjectAttachmentIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &RuntimeProjectAttachmentAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelRuntimeProjectAttachment) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelRuntimeProjectAttachment) client(ctx context.Context) (*gcp.RuntimeProjectAttachmentClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRuntimeProjectAttachmentRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building RuntimeProjectAttachment client: %w", err)
	}
	return gcpClient, err
}

type RuntimeProjectAttachmentAdapter struct {
	id        *krm.APIHubRuntimeProjectAttachmentIdentity
	gcpClient *gcp.RuntimeProjectAttachmentClient
	desired   *krm.APIHubRuntimeProjectAttachment
	actual    *pb.RuntimeProjectAttachment
}

var _ directbase.Adapter = &RuntimeProjectAttachmentAdapter{}

func (a *RuntimeProjectAttachmentAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting RuntimeProjectAttachment", "name", a.id)

	req := &pb.GetRuntimeProjectAttachmentRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetRuntimeProjectAttachment(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting RuntimeProjectAttachment %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *RuntimeProjectAttachmentAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating RuntimeProjectAttachment", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := APIHubRuntimeProjectAttachmentSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateRuntimeProjectAttachmentRequest{
		Parent:                     a.id.ParentString(),
		RuntimeProjectAttachmentId: a.id.Runtime,
		RuntimeProjectAttachment:   resource,
	}
	created, err := a.gcpClient.CreateRuntimeProjectAttachment(ctx, req)
	if err != nil {
		return fmt.Errorf("creating RuntimeProjectAttachment %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created RuntimeProjectAttachment", "name", a.id)

	status := &krm.APIHubRuntimeProjectAttachmentStatus{}
	status.ObservedState = APIHubRuntimeProjectAttachmentObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *RuntimeProjectAttachmentAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating RuntimeProjectAttachment", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := APIHubRuntimeProjectAttachmentSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareRuntimeProjectAttachment(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("APIHubRuntimeProjectAttachment resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.APIHubRuntimeProjectAttachmentStatus{}
	mapCtx = &direct.MapContext{}
	status.ObservedState = APIHubRuntimeProjectAttachmentObservedState_FromProto(mapCtx, a.actual)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareRuntimeProjectAttachment(ctx context.Context, actual, desired *pb.RuntimeProjectAttachment) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, APIHubRuntimeProjectAttachmentSpec_FromProto, APIHubRuntimeProjectAttachmentSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *RuntimeProjectAttachmentAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.APIHubRuntimeProjectAttachment{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(APIHubRuntimeProjectAttachmentSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = a.id.Location
	obj.Spec.ResourceID = direct.LazyPtr(a.id.Runtime)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Runtime)
	u.SetGroupVersionKind(krm.APIHubRuntimeProjectAttachmentGVK)
	return u, nil
}

func (a *RuntimeProjectAttachmentAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting RuntimeProjectAttachment", "name", a.id)

	req := &pb.DeleteRuntimeProjectAttachmentRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteRuntimeProjectAttachment(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting RuntimeProjectAttachment %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted RuntimeProjectAttachment", "name", a.id)
	return true, nil
}
