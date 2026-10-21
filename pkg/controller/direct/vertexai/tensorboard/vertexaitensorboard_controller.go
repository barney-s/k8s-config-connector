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

package tensorboard

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
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/export"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/label"
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
	registry.RegisterModel(krm.VertexAITensorboardGVK, NewTensorboardModel)
}

func NewTensorboardModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelTensorboard{config: *config}, nil
}

var _ directbase.Model = &modelTensorboard{}
var _ = strings.TrimPrefix

type modelTensorboard struct {
	config config.ControllerConfig
}

func (m *modelTensorboard) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.VertexAITensorboard{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.VertexAITensorboardIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &TensorboardAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelTensorboard) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.VertexAITensorboardIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &TensorboardAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelTensorboard) client(ctx context.Context) (*gcp.TensorboardClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewTensorboardRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Tensorboard client: %w", err)
	}
	return gcpClient, err
}

type TensorboardAdapter struct {
	id        *krm.VertexAITensorboardIdentity
	gcpClient *gcp.TensorboardClient
	desired   *krm.VertexAITensorboard
	actual    *pb.Tensorboard
}

var _ directbase.Adapter = &TensorboardAdapter{}

func (a *TensorboardAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Tensorboard", "name", a.id)

	req := &pb.GetTensorboardRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetTensorboard(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Tensorboard %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *TensorboardAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Tensorboard", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := VertexAITensorboardSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateTensorboardRequest{
		Parent:        a.id.ParentString(),
		TensorboardId: a.id.FromExternal,
		Tensorboard:   resource,
	}
	op, err := a.gcpClient.CreateTensorboard(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Tensorboard %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create Tensorboard %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Tensorboard", "name", a.id)

	status := &krm.VertexAITensorboardStatus{}
	status.ObservedState = VertexAITensorboardObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *TensorboardAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Tensorboard", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := VertexAITensorboardSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareTensorboard(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateTensorboardRequest{
			Tensorboard: desired,
			UpdateMask:  updateMask,
		}
		op, err := a.gcpClient.UpdateTensorboard(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Tensorboard %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update Tensorboard %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Tensorboard", "name", a.id)

		status := &krm.VertexAITensorboardStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = VertexAITensorboardObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareTensorboard(ctx context.Context, actual, desired *pb.Tensorboard) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, VertexAITensorboardSpec_FromProto, VertexAITensorboardSpec_ToProto)
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

func (a *TensorboardAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.VertexAITensorboard{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(VertexAITensorboardSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.FromExternal)
	u.SetGroupVersionKind(krm.VertexAITensorboardGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *TensorboardAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Tensorboard", "name", a.id)

	req := &pb.DeleteTensorboardRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteTensorboard(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Tensorboard %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Tensorboard", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete Tensorboard %s: %w", a.id, err)
		}
	}
	return true, nil
}
