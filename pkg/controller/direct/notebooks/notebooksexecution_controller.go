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

package notebooks

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/notebooks/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/notebooks/apiv1"
	pb "cloud.google.com/go/notebooks/apiv1/notebookspb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.NotebooksExecutionGVK, NewExecutionModel)
}

func NewExecutionModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelExecution{config: *config}, nil
}

var _ directbase.Model = &modelExecution{}
var _ = strings.TrimPrefix

type modelExecution struct {
	config config.ControllerConfig
}

func (m *modelExecution) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.NotebooksExecution{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id, err := krm.NewExecutionIdentity(ctx, reader, obj)
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
	return &ExecutionAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelExecution) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.NotebooksExecutionIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ExecutionAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelExecution) client(ctx context.Context) (*gcp.NotebookClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.GRPCClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewNotebookClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Execution client: %w", err)
	}
	return gcpClient, err
}

type ExecutionAdapter struct {
	id        *krm.NotebooksExecutionIdentity
	gcpClient *gcp.NotebookClient
	desired   *krm.NotebooksExecution
	actual    *pb.Execution
}

var _ directbase.Adapter = &ExecutionAdapter{}

func (a *ExecutionAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Execution", "name", a.id)

	req := &pb.GetExecutionRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetExecution(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Execution %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ExecutionAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Execution", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := NotebooksExecutionSpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateExecutionRequest{
		Parent:      a.id.ParentString(),
		ExecutionId: a.id.ID(),
		Execution:   resource,
	}
	op, err := a.gcpClient.CreateExecution(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Execution %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create Execution %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Execution", "name", a.id)

	status := &krm.NotebooksExecutionStatus{}
	status.ObservedState = NotebooksExecutionObservedState_v1alpha1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ExecutionAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Execution", "name", a.id)

	desired, diffs, updateMask, err := compareExecution(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	_ = desired
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("NotebooksExecution resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.NotebooksExecutionStatus{}
	mapCtx := &direct.MapContext{}
	status.ObservedState = NotebooksExecutionObservedState_v1alpha1_FromProto(mapCtx, a.actual)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareExecution(ctx context.Context, u *unstructured.Unstructured, actual *pb.Execution, desiredSpec *krm.NotebooksExecutionSpec) (*pb.Execution, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		NotebooksExecutionSpec_v1alpha1_FromProto,
		NotebooksExecutionSpec_v1alpha1_ToProto,
		nil,
	)
}

func (a *ExecutionAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.NotebooksExecution{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(NotebooksExecutionSpec_v1alpha1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = &a.id.Location

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.NotebooksExecutionGVK)
	return u, nil
}

func (a *ExecutionAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Execution", "name", a.id)

	req := &pb.DeleteExecutionRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteExecution(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Execution %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Execution", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete Execution %s: %w", a.id, err)
		}
	}
	return true, nil
}
