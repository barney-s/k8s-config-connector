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

package featuregroup

import (
	"context"
	"fmt"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/vertexai/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/aiplatform/apiv1beta1"
	pb "cloud.google.com/go/aiplatform/apiv1beta1/aiplatformpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.VertexAIFeatureGroupGVK, NewFeatureGroupModel)
}

func NewFeatureGroupModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelFeatureGroup{config: *config}, nil
}

var _ directbase.Model = &modelFeatureGroup{}
var _ = strings.TrimPrefix

type modelFeatureGroup struct {
	config config.ControllerConfig
}

func (m *modelFeatureGroup) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.VertexAIFeatureGroup{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.VertexAIFeatureGroupIdentity)

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &FeatureGroupAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelFeatureGroup) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelFeatureGroup) client(ctx context.Context) (*gcp.FeatureRegistryClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.GRPCClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewFeatureRegistryClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building FeatureGroup client: %w", err)
	}
	return gcpClient, err
}

type FeatureGroupAdapter struct {
	id        *krm.VertexAIFeatureGroupIdentity
	gcpClient *gcp.FeatureRegistryClient
	desired   *krm.VertexAIFeatureGroup
	actual    *pb.FeatureGroup
}

var _ directbase.Adapter = &FeatureGroupAdapter{}

func (a *FeatureGroupAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting FeatureGroup", "name", a.id)

	req := &pb.GetFeatureGroupRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetFeatureGroup(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting FeatureGroup %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *FeatureGroupAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating FeatureGroup", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := VertexAIFeatureGroupSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateFeatureGroupRequest{
		Parent:         a.id.ParentString(),
		FeatureGroupId: a.id.FeatureGroup,
		FeatureGroup:   resource,
	}
	op, err := a.gcpClient.CreateFeatureGroup(ctx, req)
	if err != nil {
		return fmt.Errorf("creating FeatureGroup %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create FeatureGroup %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created FeatureGroup", "name", a.id)

	status := &krm.VertexAIFeatureGroupStatus{}
	status.ObservedState = VertexAIFeatureGroupObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *FeatureGroupAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating FeatureGroup", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := VertexAIFeatureGroupSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareFeatureGroup(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateFeatureGroupRequest{
			FeatureGroup: desired,
			UpdateMask:   updateMask,
		}
		op, err := a.gcpClient.UpdateFeatureGroup(ctx, req)
		if err != nil {
			return fmt.Errorf("updating FeatureGroup %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update FeatureGroup %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated FeatureGroup", "name", a.id)

		status := &krm.VertexAIFeatureGroupStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = VertexAIFeatureGroupObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareFeatureGroup(ctx context.Context, actual, desired *pb.FeatureGroup) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, VertexAIFeatureGroupSpec_FromProto, VertexAIFeatureGroupSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *FeatureGroupAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.VertexAIFeatureGroup{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(VertexAIFeatureGroupSpec_FromProto(mapCtx, a.actual))
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
	u.SetName(a.id.FeatureGroup)
	u.SetGroupVersionKind(krm.VertexAIFeatureGroupGVK)
	return u, nil
}

func (a *FeatureGroupAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting FeatureGroup", "name", a.id)

	req := &pb.DeleteFeatureGroupRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteFeatureGroup(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting FeatureGroup %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted FeatureGroup", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete FeatureGroup %s: %w", a.id, err)
		}
	}
	return true, nil
}
