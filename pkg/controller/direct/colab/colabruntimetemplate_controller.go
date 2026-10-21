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
	registry.RegisterModel(krm.ColabRuntimeTemplateGVK, NewNotebookRuntimeTemplateModel)
}

func NewNotebookRuntimeTemplateModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelNotebookRuntimeTemplate{config: *config}, nil
}

var _ directbase.Model = &modelNotebookRuntimeTemplate{}
var _ = strings.TrimPrefix

type modelNotebookRuntimeTemplate struct {
	config config.ControllerConfig
}

func (m *modelNotebookRuntimeTemplate) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.ColabRuntimeTemplate{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.NotebookRuntimeTemplateIdentity{}
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &NotebookRuntimeTemplateAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelNotebookRuntimeTemplate) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelNotebookRuntimeTemplate) client(ctx context.Context) (*gcp.NotebookClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewNotebookRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building NotebookRuntimeTemplate client: %w", err)
	}
	return gcpClient, err
}

type NotebookRuntimeTemplateAdapter struct {
	id        *krm.NotebookRuntimeTemplateIdentity
	gcpClient *gcp.NotebookClient
	desired   *krm.ColabRuntimeTemplate
	actual    *pb.NotebookRuntimeTemplate
}

var _ directbase.Adapter = &NotebookRuntimeTemplateAdapter{}

func (a *NotebookRuntimeTemplateAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting NotebookRuntimeTemplate", "name", a.id)

	req := &pb.GetNotebookRuntimeTemplateRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetNotebookRuntimeTemplate(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting NotebookRuntimeTemplate %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *NotebookRuntimeTemplateAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating NotebookRuntimeTemplate", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := ColabRuntimeTemplateSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateNotebookRuntimeTemplateRequest{
		Parent:                    a.id.Parent().String(),
		NotebookRuntimeTemplateId: a.id.ID(),
		NotebookRuntimeTemplate:   resource,
	}
	op, err := a.gcpClient.CreateNotebookRuntimeTemplate(ctx, req)
	if err != nil {
		return fmt.Errorf("creating NotebookRuntimeTemplate %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create NotebookRuntimeTemplate %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created NotebookRuntimeTemplate", "name", a.id)

	status := &krm.ColabRuntimeTemplateStatus{}
	status.ObservedState = ColabRuntimeTemplateObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *NotebookRuntimeTemplateAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating NotebookRuntimeTemplate", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := ColabRuntimeTemplateSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareNotebookRuntimeTemplate(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateNotebookRuntimeTemplateRequest{
			NotebookRuntimeTemplate: desired,
			UpdateMask:              updateMask,
		}
		op, err := a.gcpClient.UpdateNotebookRuntimeTemplate(ctx, req)
		if err != nil {
			return fmt.Errorf("updating NotebookRuntimeTemplate %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update NotebookRuntimeTemplate %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated NotebookRuntimeTemplate", "name", a.id)

		status := &krm.ColabRuntimeTemplateStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = ColabRuntimeTemplateObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareNotebookRuntimeTemplate(ctx context.Context, actual, desired *pb.NotebookRuntimeTemplate) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, ColabRuntimeTemplateSpec_FromProto, ColabRuntimeTemplateSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *NotebookRuntimeTemplateAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.ColabRuntimeTemplate{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(ColabRuntimeTemplateSpec_FromProto(mapCtx, a.actual))
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
	u.SetGroupVersionKind(krm.ColabRuntimeTemplateGVK)
	return u, nil
}

func (a *NotebookRuntimeTemplateAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting NotebookRuntimeTemplate", "name", a.id)

	req := &pb.DeleteNotebookRuntimeTemplateRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteNotebookRuntimeTemplate(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting NotebookRuntimeTemplate %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted NotebookRuntimeTemplate", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete NotebookRuntimeTemplate %s: %w", a.id, err)
		}
	}
	return true, nil
}
