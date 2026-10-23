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

package vertexai

import (
	"context"
	"fmt"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/vertexai/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/aiplatform/apiv1beta1"
	vertexaipb "cloud.google.com/go/aiplatform/apiv1beta1/aiplatformpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.VertexAIMetadataStoreGVK, NewMetadataStoreModel)
}

func NewMetadataStoreModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelMetadataStore{config: *config}, nil
}

var _ directbase.Model = &modelMetadataStore{}
var _ = strings.TrimPrefix

type modelMetadataStore struct {
	config config.ControllerConfig
}

func (m *modelMetadataStore) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.VertexAIMetadataStore{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id, err := krm.NewMetadataStoreIdentity(ctx, reader, obj)
	if err != nil {
		return nil, err
	}
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &MetadataStoreAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelMetadataStore) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelMetadataStore) client(ctx context.Context) (*gcp.MetadataClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewMetadataRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building MetadataStore client: %w", err)
	}
	return gcpClient, err
}

type MetadataStoreAdapter struct {
	id        *krm.MetadataStoreIdentity
	gcpClient *gcp.MetadataClient
	desired   *krm.VertexAIMetadataStore
	actual    *vertexaipb.MetadataStore
}

var _ directbase.Adapter = &MetadataStoreAdapter{}

func (a *MetadataStoreAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting MetadataStore", "name", a.id)

	req := &vertexaipb.GetMetadataStoreRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetMetadataStore(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting MetadataStore %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *MetadataStoreAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating MetadataStore", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := VertexAIMetadataStoreSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &vertexaipb.CreateMetadataStoreRequest{
		Parent:          a.id.Parent().String(),
		MetadataStoreId: a.id.ID(),
		MetadataStore:   resource,
	}
	op, err := a.gcpClient.CreateMetadataStore(ctx, req)
	if err != nil {
		return fmt.Errorf("creating MetadataStore %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create MetadataStore %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created MetadataStore", "name", a.id)

	status := &krm.VertexAIMetadataStoreStatus{}
	status.ObservedState = VertexAIMetadataStoreObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *MetadataStoreAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating MetadataStore", "name", a.id)

	desired, diffs, updateMask, err := compareMetadataStore(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	_ = desired
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("VertexAIMetadataStore resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.VertexAIMetadataStoreStatus{}
	mapCtx := &direct.MapContext{}
	status.ObservedState = VertexAIMetadataStoreObservedState_FromProto(mapCtx, a.actual)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareMetadataStore(ctx context.Context, u *unstructured.Unstructured, actual *vertexaipb.MetadataStore, desiredSpec *krm.VertexAIMetadataStoreSpec) (*vertexaipb.MetadataStore, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		VertexAIMetadataStoreSpec_FromProto,
		VertexAIMetadataStoreSpec_ToProto,
		nil,
	)
}

func (a *MetadataStoreAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.VertexAIMetadataStore{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(VertexAIMetadataStoreSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Parent().ProjectID}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.VertexAIMetadataStoreGVK)
	return u, nil
}

func (a *MetadataStoreAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting MetadataStore", "name", a.id)

	req := &vertexaipb.DeleteMetadataStoreRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteMetadataStore(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting MetadataStore %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted MetadataStore", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete MetadataStore %s: %w", a.id, err)
		}
	}
	return true, nil
}
