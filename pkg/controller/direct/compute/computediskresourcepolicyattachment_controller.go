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

package compute

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/compute/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/compute/apiv1"
	pb "cloud.google.com/go/compute/apiv1/computepb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.ComputeDiskResourcePolicyAttachmentGVK, NewComputeDiskResourcePolicyAttachmentModel)
}

func NewComputeDiskResourcePolicyAttachmentModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelComputeDiskResourcePolicyAttachment{config: *config}, nil
}

var _ directbase.Model = &modelComputeDiskResourcePolicyAttachment{}
var _ = strings.TrimPrefix

type modelComputeDiskResourcePolicyAttachment struct {
	config config.ControllerConfig
}

func (m *modelComputeDiskResourcePolicyAttachment) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.ComputeDiskResourcePolicyAttachment{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.ComputeDiskResourcePolicyAttachmentIdentity)

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ComputeDiskResourcePolicyAttachmentAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelComputeDiskResourcePolicyAttachment) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelComputeDiskResourcePolicyAttachment) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ComputeDiskResourcePolicyAttachment client: %w", err)
	}
	return gcpClient, err
}

type ComputeDiskResourcePolicyAttachmentAdapter struct {
	id        *krm.ComputeDiskResourcePolicyAttachmentIdentity
	gcpClient *gcp.Client
	desired   *krm.ComputeDiskResourcePolicyAttachment
	actual    *pb.ComputeDiskResourcePolicyAttachment
}

var _ directbase.Adapter = &ComputeDiskResourcePolicyAttachmentAdapter{}

func (a *ComputeDiskResourcePolicyAttachmentAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ComputeDiskResourcePolicyAttachment", "name", a.id)

	req := &pb.GetComputeDiskResourcePolicyAttachmentRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetComputeDiskResourcePolicyAttachment(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ComputeDiskResourcePolicyAttachment %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ComputeDiskResourcePolicyAttachmentAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ComputeDiskResourcePolicyAttachment", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := ComputeDiskResourcePolicyAttachmentSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateComputeDiskResourcePolicyAttachmentRequest{
		Parent:                                a.id.ParentString(),
		ComputeDiskResourcePolicyAttachmentId: a.id.Zone,
		ComputeDiskResourcePolicyAttachment:   resource,
	}
	op, err := a.gcpClient.CreateComputeDiskResourcePolicyAttachment(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ComputeDiskResourcePolicyAttachment %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create ComputeDiskResourcePolicyAttachment %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ComputeDiskResourcePolicyAttachment", "name", a.id)

	status := &krm.ComputeDiskResourcePolicyAttachmentStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ComputeDiskResourcePolicyAttachmentAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ComputeDiskResourcePolicyAttachment", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := ComputeDiskResourcePolicyAttachmentSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareComputeDiskResourcePolicyAttachment(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("ComputeDiskResourcePolicyAttachment resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.ComputeDiskResourcePolicyAttachmentStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareComputeDiskResourcePolicyAttachment(ctx context.Context, actual, desired *pb.ComputeDiskResourcePolicyAttachment) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, ComputeDiskResourcePolicyAttachmentSpec_FromProto, ComputeDiskResourcePolicyAttachmentSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *ComputeDiskResourcePolicyAttachmentAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.ComputeDiskResourcePolicyAttachment{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(ComputeDiskResourcePolicyAttachmentSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.ResourceID = direct.LazyPtr(a.id.Zone)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Zone)
	u.SetGroupVersionKind(krm.ComputeDiskResourcePolicyAttachmentGVK)
	return u, nil
}

func (a *ComputeDiskResourcePolicyAttachmentAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deletion not supported for ComputeDiskResourcePolicyAttachment, removing finalizer without deleting GCP resource", "name", a.id)
	return true, nil
}
