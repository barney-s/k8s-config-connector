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

package aiplatform

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/aiplatform/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/export"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/label"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/aiplatform/apiv1"
	pb "cloud.google.com/go/aiplatform/apiv1/aiplatformpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.AIPlatformPersistentResourceGVK, NewPersistentResourceModel)
}

func NewPersistentResourceModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelPersistentResource{config: *config}, nil
}

var _ directbase.Model = &modelPersistentResource{}
var _ = strings.TrimPrefix

type modelPersistentResource struct {
	config config.ControllerConfig
}

func (m *modelPersistentResource) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.AIPlatformPersistentResource{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.AIPlatformPersistentResourceIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &PersistentResourceAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelPersistentResource) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.AIPlatformPersistentResourceIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &PersistentResourceAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelPersistentResource) client(ctx context.Context) (*gcp.PersistentResourceClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.GRPCClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewPersistentResourceClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building PersistentResource client: %w", err)
	}
	return gcpClient, err
}

type PersistentResourceAdapter struct {
	id        *krm.AIPlatformPersistentResourceIdentity
	gcpClient *gcp.PersistentResourceClient
	desired   *krm.AIPlatformPersistentResource
	actual    *pb.PersistentResource
}

var _ directbase.Adapter = &PersistentResourceAdapter{}

func (a *PersistentResourceAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting PersistentResource", "name", a.id)

	req := &pb.GetPersistentResourceRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetPersistentResource(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting PersistentResource %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *PersistentResourceAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating PersistentResource", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := AIPlatformPersistentResourceSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreatePersistentResourceRequest{
		Parent:               a.id.ParentString(),
		PersistentResourceId: a.id.PersistentResource,
		PersistentResource:   resource,
	}
	op, err := a.gcpClient.CreatePersistentResource(ctx, req)
	if err != nil {
		return fmt.Errorf("creating PersistentResource %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create PersistentResource %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created PersistentResource", "name", a.id)

	status := &krm.AIPlatformPersistentResourceStatus{}
	status.ObservedState = AIPlatformPersistentResourceObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *PersistentResourceAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating PersistentResource", "name", a.id)

	desired, diffs, updateMask, err := comparePersistentResource(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	_ = desired
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("AIPlatformPersistentResource resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.AIPlatformPersistentResourceStatus{}
	mapCtx := &direct.MapContext{}
	status.ObservedState = AIPlatformPersistentResourceObservedState_FromProto(mapCtx, a.actual)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func comparePersistentResource(ctx context.Context, u *unstructured.Unstructured, actual *pb.PersistentResource, desiredSpec *krm.AIPlatformPersistentResourceSpec) (*pb.PersistentResource, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	return common.CompareGreenfieldSpecAndLabels(
		ctx,
		u,
		desiredSpec,
		actual,
		"labels",
		AIPlatformPersistentResourceSpec_FromProto,
		AIPlatformPersistentResourceSpec_ToProto,
		nil,
	)
}

func (a *PersistentResourceAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.AIPlatformPersistentResource{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(AIPlatformPersistentResourceSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{Name: a.id.Project}
	obj.Spec.Location = &a.id.Location
	obj.Spec.ResourceID = &a.id.PersistentResource

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.PersistentResource)
	u.SetGroupVersionKind(krm.AIPlatformPersistentResourceGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *PersistentResourceAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting PersistentResource", "name", a.id)

	req := &pb.DeletePersistentResourceRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeletePersistentResource(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting PersistentResource %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted PersistentResource", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete PersistentResource %s: %w", a.id, err)
		}
	}
	return true, nil
}
