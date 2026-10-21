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

package colab

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/colab/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
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
	registry.RegisterModel(krm.ColabRuntimeGVK, NewNotebookRuntimeModel)
}

func NewNotebookRuntimeModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelNotebookRuntime{config: *config}, nil
}

var _ directbase.Model = &modelNotebookRuntime{}
var _ = strings.TrimPrefix

type modelNotebookRuntime struct {
	config config.ControllerConfig
}

func (m *modelNotebookRuntime) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.ColabRuntime{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.NotebookRuntimeIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &NotebookRuntimeAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelNotebookRuntime) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelNotebookRuntime) client(ctx context.Context) (*gcp.NotebookClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewNotebookRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building NotebookRuntime client: %w", err)
	}
	return gcpClient, err
}

type NotebookRuntimeAdapter struct {
	id        *krm.NotebookRuntimeIdentity
	gcpClient *gcp.NotebookClient
	desired   *krm.ColabRuntime
	actual    *pb.NotebookRuntime
}

var _ directbase.Adapter = &NotebookRuntimeAdapter{}

func (a *NotebookRuntimeAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting NotebookRuntime", "name", a.id)

	req := &pb.GetNotebookRuntimeRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetNotebookRuntime(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting NotebookRuntime %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *NotebookRuntimeAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating NotebookRuntime", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := ColabRuntimeSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateNotebookRuntimeRequest{
		Parent:            a.id.Parent().String(),
		NotebookRuntimeId: a.id.ID(),
		NotebookRuntime:   resource,
	}
	op, err := a.gcpClient.CreateNotebookRuntime(ctx, req)
	if err != nil {
		return fmt.Errorf("creating NotebookRuntime %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create NotebookRuntime %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created NotebookRuntime", "name", a.id)

	status := &krm.ColabRuntimeStatus{}
	status.ObservedState = ColabRuntimeObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *NotebookRuntimeAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating NotebookRuntime", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := ColabRuntimeSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareNotebookRuntime(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("ColabRuntime resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.ColabRuntimeStatus{}
	mapCtx = &direct.MapContext{}
	status.ObservedState = ColabRuntimeObservedState_FromProto(mapCtx, a.actual)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareNotebookRuntime(ctx context.Context, actual, desired *pb.NotebookRuntime) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, ColabRuntimeSpec_FromProto, ColabRuntimeSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *NotebookRuntimeAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.ColabRuntime{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(ColabRuntimeSpec_FromProto(mapCtx, a.actual))
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
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.ColabRuntimeGVK)
	return u, nil
}

func (a *NotebookRuntimeAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting NotebookRuntime", "name", a.id)

	req := &pb.DeleteNotebookRuntimeRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteNotebookRuntime(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting NotebookRuntime %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted NotebookRuntime", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete NotebookRuntime %s: %w", a.id, err)
		}
	}
	return true, nil
}
