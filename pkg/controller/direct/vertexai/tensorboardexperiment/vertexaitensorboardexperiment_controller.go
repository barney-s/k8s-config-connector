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

package tensorboardexperiment

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
	registry.RegisterModel(krm.VertexAITensorboardExperimentGVK, NewTensorboardExperimentModel)
}

func NewTensorboardExperimentModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelTensorboardExperiment{config: *config}, nil
}

var _ directbase.Model = &modelTensorboardExperiment{}
var _ = strings.TrimPrefix

type modelTensorboardExperiment struct {
	config config.ControllerConfig
}

func (m *modelTensorboardExperiment) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.VertexAITensorboardExperiment{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.VertexAITensorboardExperimentIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &TensorboardExperimentAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelTensorboardExperiment) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelTensorboardExperiment) client(ctx context.Context) (*gcp.TensorboardClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewTensorboardRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building TensorboardExperiment client: %w", err)
	}
	return gcpClient, err
}

type TensorboardExperimentAdapter struct {
	id        *krm.VertexAITensorboardExperimentIdentity
	gcpClient *gcp.TensorboardClient
	desired   *krm.VertexAITensorboardExperiment
	actual    *pb.TensorboardExperiment
}

var _ directbase.Adapter = &TensorboardExperimentAdapter{}

func (a *TensorboardExperimentAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting TensorboardExperiment", "name", a.id)

	req := &pb.GetTensorboardExperimentRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetTensorboardExperiment(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting TensorboardExperiment %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *TensorboardExperimentAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating TensorboardExperiment", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := VertexAITensorboardExperimentSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateTensorboardExperimentRequest{
		Parent:                  a.id.ParentString(),
		TensorboardExperimentId: a.id.Experiment,
		TensorboardExperiment:   resource,
	}
	op, err := a.gcpClient.CreateTensorboardExperiment(ctx, req)
	if err != nil {
		return fmt.Errorf("creating TensorboardExperiment %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create TensorboardExperiment %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created TensorboardExperiment", "name", a.id)

	status := &krm.VertexAITensorboardExperimentStatus{}
	status.ObservedState = VertexAITensorboardExperimentObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *TensorboardExperimentAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating TensorboardExperiment", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := VertexAITensorboardExperimentSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareTensorboardExperiment(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateTensorboardExperimentRequest{
			TensorboardExperiment: desired,
			UpdateMask:            updateMask,
		}
		op, err := a.gcpClient.UpdateTensorboardExperiment(ctx, req)
		if err != nil {
			return fmt.Errorf("updating TensorboardExperiment %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update TensorboardExperiment %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated TensorboardExperiment", "name", a.id)

		status := &krm.VertexAITensorboardExperimentStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = VertexAITensorboardExperimentObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareTensorboardExperiment(ctx context.Context, actual, desired *pb.TensorboardExperiment) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, VertexAITensorboardExperimentSpec_FromProto, VertexAITensorboardExperimentSpec_ToProto)
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

func (a *TensorboardExperimentAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.VertexAITensorboardExperiment{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(VertexAITensorboardExperimentSpec_FromProto(mapCtx, a.actual))
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
	u.SetName(a.id.Experiment)
	u.SetGroupVersionKind(krm.VertexAITensorboardExperimentGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *TensorboardExperimentAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting TensorboardExperiment", "name", a.id)

	req := &pb.DeleteTensorboardExperimentRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteTensorboardExperiment(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting TensorboardExperiment %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted TensorboardExperiment", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete TensorboardExperiment %s: %w", a.id, err)
		}
	}
	return true, nil
}
