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

package vertexaifeatureonlinestore

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/aiplatform/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/export"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/label"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
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
	registry.RegisterModel(krm.VertexAIFeatureOnlineStoreGVK, NewFeatureOnlineStoreModel)
}

func NewFeatureOnlineStoreModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelFeatureOnlineStore{config: *config}, nil
}

var _ directbase.Model = &modelFeatureOnlineStore{}
var _ = strings.TrimPrefix

type modelFeatureOnlineStore struct {
	config config.ControllerConfig
}

func (m *modelFeatureOnlineStore) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.VertexAIFeatureOnlineStore{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.FeatureOnlineStoreIdentity)

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &FeatureOnlineStoreAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelFeatureOnlineStore) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelFeatureOnlineStore) client(ctx context.Context) (*gcp.FeatureOnlineStoreAdminClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.GRPCClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewFeatureOnlineStoreAdminClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building FeatureOnlineStore client: %w", err)
	}
	return gcpClient, err
}

type FeatureOnlineStoreAdapter struct {
	id        *krm.FeatureOnlineStoreIdentity
	gcpClient *gcp.FeatureOnlineStoreAdminClient
	desired   *krm.VertexAIFeatureOnlineStore
	actual    *pb.FeatureOnlineStore
}

var _ directbase.Adapter = &FeatureOnlineStoreAdapter{}

func (a *FeatureOnlineStoreAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting FeatureOnlineStore", "name", a.id)

	req := &pb.GetFeatureOnlineStoreRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetFeatureOnlineStore(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting FeatureOnlineStore %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *FeatureOnlineStoreAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating FeatureOnlineStore", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := VertexAIFeatureOnlineStoreSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateFeatureOnlineStoreRequest{
		Parent:               a.id.Parent().String(),
		FeatureOnlineStoreId: a.id.FeatureOnlineStore,
		FeatureOnlineStore:   resource,
	}
	op, err := a.gcpClient.CreateFeatureOnlineStore(ctx, req)
	if err != nil {
		return fmt.Errorf("creating FeatureOnlineStore %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create FeatureOnlineStore %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created FeatureOnlineStore", "name", a.id)

	status := &krm.VertexAIFeatureOnlineStoreStatus{}
	status.ObservedState = VertexAIFeatureOnlineStoreObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *FeatureOnlineStoreAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating FeatureOnlineStore", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := VertexAIFeatureOnlineStoreSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareFeatureOnlineStore(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateFeatureOnlineStoreRequest{
			FeatureOnlineStore: desired,
			UpdateMask:         updateMask,
		}
		op, err := a.gcpClient.UpdateFeatureOnlineStore(ctx, req)
		if err != nil {
			return fmt.Errorf("updating FeatureOnlineStore %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update FeatureOnlineStore %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated FeatureOnlineStore", "name", a.id)

		status := &krm.VertexAIFeatureOnlineStoreStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = VertexAIFeatureOnlineStoreObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareFeatureOnlineStore(ctx context.Context, actual, desired *pb.FeatureOnlineStore) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, VertexAIFeatureOnlineStoreSpec_FromProto, VertexAIFeatureOnlineStoreSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	maskedActual.Labels = actual.Labels
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *FeatureOnlineStoreAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.VertexAIFeatureOnlineStore{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(VertexAIFeatureOnlineStoreSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.FeatureOnlineStore)
	u.SetGroupVersionKind(krm.VertexAIFeatureOnlineStoreGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *FeatureOnlineStoreAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting FeatureOnlineStore", "name", a.id)

	req := &pb.DeleteFeatureOnlineStoreRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteFeatureOnlineStore(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting FeatureOnlineStore %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted FeatureOnlineStore", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete FeatureOnlineStore %s: %w", a.id, err)
		}
	}
	return true, nil
}
