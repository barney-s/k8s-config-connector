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

package examplestore

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
	vertexaipb "cloud.google.com/go/aiplatform/apiv1beta1/aiplatformpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.VertexAIExampleStoreGVK, NewExampleStoreModel)
}

func NewExampleStoreModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelExampleStore{config: *config}, nil
}

var _ directbase.Model = &modelExampleStore{}
var _ = strings.TrimPrefix

type modelExampleStore struct {
	config config.ControllerConfig
}

func (m *modelExampleStore) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.VertexAIExampleStore{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.VertexAIExampleStoreIdentity)

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ExampleStoreAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelExampleStore) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelExampleStore) client(ctx context.Context) (*gcp.ExampleStoreClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewExampleStoreRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ExampleStore client: %w", err)
	}
	return gcpClient, err
}

type ExampleStoreAdapter struct {
	id        *krm.VertexAIExampleStoreIdentity
	gcpClient *gcp.ExampleStoreClient
	desired   *krm.VertexAIExampleStore
	actual    *vertexaipb.ExampleStore
}

var _ directbase.Adapter = &ExampleStoreAdapter{}

func (a *ExampleStoreAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ExampleStore", "name", a.id)

	req := &vertexaipb.GetExampleStoreRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetExampleStore(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ExampleStore %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ExampleStoreAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ExampleStore", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := VertexAIExampleStoreSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &vertexaipb.CreateExampleStoreRequest{
		Parent:         a.id.ParentString(),
		ExampleStoreId: a.id.ExampleStore,
		ExampleStore:   resource,
	}
	op, err := a.gcpClient.CreateExampleStore(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ExampleStore %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create ExampleStore %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ExampleStore", "name", a.id)

	status := &krm.VertexAIExampleStoreStatus{}
	status.ObservedState = VertexAIExampleStoreObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ExampleStoreAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ExampleStore", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := VertexAIExampleStoreSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareExampleStore(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &vertexaipb.UpdateExampleStoreRequest{
			ExampleStore: desired,
			UpdateMask:   updateMask,
		}
		op, err := a.gcpClient.UpdateExampleStore(ctx, req)
		if err != nil {
			return fmt.Errorf("updating ExampleStore %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update ExampleStore %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated ExampleStore", "name", a.id)

		status := &krm.VertexAIExampleStoreStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = VertexAIExampleStoreObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareExampleStore(ctx context.Context, actual, desired *vertexaipb.ExampleStore) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, VertexAIExampleStoreSpec_FromProto, VertexAIExampleStoreSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *ExampleStoreAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.VertexAIExampleStore{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(VertexAIExampleStoreSpec_FromProto(mapCtx, a.actual))
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
	u.SetName(a.id.ExampleStore)
	u.SetGroupVersionKind(krm.VertexAIExampleStoreGVK)
	return u, nil
}

func (a *ExampleStoreAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting ExampleStore", "name", a.id)

	req := &vertexaipb.DeleteExampleStoreRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteExampleStore(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting ExampleStore %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted ExampleStore", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete ExampleStore %s: %w", a.id, err)
		}
	}
	return true, nil
}
