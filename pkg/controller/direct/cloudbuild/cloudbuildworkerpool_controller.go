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

package cloudbuild

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/cloudbuild/v1beta1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/cloudbuild/apiv1/v2"
	cloudbuildpb "cloud.google.com/go/cloudbuild/apiv1/v2/cloudbuildpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.CloudbuildworkerpoolGVK, NewWorkerPoolModel)
}

func NewWorkerPoolModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelWorkerPool{config: *config}, nil
}

var _ directbase.Model = &modelWorkerPool{}
var _ = strings.TrimPrefix

type modelWorkerPool struct {
	config config.ControllerConfig
}

func (m *modelWorkerPool) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.Cloudbuildworkerpool{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &CloudBuildWorkerPoolIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &WorkerPoolAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelWorkerPool) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelWorkerPool) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building WorkerPool client: %w", err)
	}
	return gcpClient, err
}

type WorkerPoolAdapter struct {
	id        *CloudBuildWorkerPoolIdentity
	gcpClient *gcp.Client
	desired   *krm.Cloudbuildworkerpool
	actual    *cloudbuildpb.WorkerPool
}

var _ directbase.Adapter = &WorkerPoolAdapter{}

func (a *WorkerPoolAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting WorkerPool", "name", a.id)

	req := &cloudbuildpb.GetWorkerPoolRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetWorkerPool(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting WorkerPool %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *WorkerPoolAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating WorkerPool", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := CloudbuildworkerpoolSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &cloudbuildpb.CreateWorkerPoolRequest{
		Parent:       a.id.ParentString(),
		WorkerPoolId: a.id.FullyQualifiedName,
		WorkerPool:   resource,
	}
	op, err := a.gcpClient.CreateWorkerPool(ctx, req)
	if err != nil {
		return fmt.Errorf("creating WorkerPool %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create WorkerPool %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created WorkerPool", "name", a.id)

	status := &krm.CloudbuildworkerpoolStatus{}
	status.ObservedState = CloudbuildworkerpoolObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *WorkerPoolAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating WorkerPool", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := CloudbuildworkerpoolSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareWorkerPool(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &cloudbuildpb.UpdateWorkerPoolRequest{
			WorkerPool: desired,
			UpdateMask: updateMask,
		}
		op, err := a.gcpClient.UpdateWorkerPool(ctx, req)
		if err != nil {
			return fmt.Errorf("updating WorkerPool %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update WorkerPool %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated WorkerPool", "name", a.id)

		status := &krm.CloudbuildworkerpoolStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = CloudbuildworkerpoolObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareWorkerPool(ctx context.Context, actual, desired *cloudbuildpb.WorkerPool) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, CloudbuildworkerpoolSpec_FromProto, CloudbuildworkerpoolSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *WorkerPoolAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.Cloudbuildworkerpool{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(CloudbuildworkerpoolSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = a.id.Location
	obj.Spec.ResourceID = direct.LazyPtr(a.id.FullyQualifiedName)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.FullyQualifiedName)
	u.SetGroupVersionKind(krm.CloudbuildworkerpoolGVK)
	return u, nil
}

func (a *WorkerPoolAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting WorkerPool", "name", a.id)

	req := &cloudbuildpb.DeleteWorkerPoolRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteWorkerPool(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting WorkerPool %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted WorkerPool", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete WorkerPool %s: %w", a.id, err)
		}
	}
	return true, nil
}
