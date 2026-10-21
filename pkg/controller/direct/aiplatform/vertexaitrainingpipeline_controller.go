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

package aiplatform

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/aiplatform/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
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
	registry.RegisterModel(krm.VertexAITrainingPipelineGVK, NewTrainingPipelineModel)
}

func NewTrainingPipelineModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelTrainingPipeline{config: *config}, nil
}

var _ directbase.Model = &modelTrainingPipeline{}
var _ = strings.TrimPrefix

type modelTrainingPipeline struct {
	config config.ControllerConfig
}

func (m *modelTrainingPipeline) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.VertexAITrainingPipeline{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.VertexAITrainingPipelineIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &TrainingPipelineAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelTrainingPipeline) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelTrainingPipeline) client(ctx context.Context) (*gcp.PipelineClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.GRPCClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewPipelineClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building TrainingPipeline client: %w", err)
	}
	return gcpClient, err
}

type TrainingPipelineAdapter struct {
	id        *krm.VertexAITrainingPipelineIdentity
	gcpClient *gcp.PipelineClient
	desired   *krm.VertexAITrainingPipeline
	actual    *pb.TrainingPipeline
}

var _ directbase.Adapter = &TrainingPipelineAdapter{}

func (a *TrainingPipelineAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting TrainingPipeline", "name", a.id)

	req := &pb.GetTrainingPipelineRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetTrainingPipeline(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting TrainingPipeline %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *TrainingPipelineAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating TrainingPipeline", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := VertexAITrainingPipelineSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateTrainingPipelineRequest{
		Parent:             a.id.ParentString(),
		TrainingPipelineId: a.id.TrainingPipeline,
		TrainingPipeline:   resource,
	}
	op, err := a.gcpClient.CreateTrainingPipeline(ctx, req)
	if err != nil {
		return fmt.Errorf("creating TrainingPipeline %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create TrainingPipeline %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created TrainingPipeline", "name", a.id)

	status := &krm.VertexAITrainingPipelineStatus{}
	status.ObservedState = VertexAITrainingPipelineObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *TrainingPipelineAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating TrainingPipeline", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := VertexAITrainingPipelineSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareTrainingPipeline(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("VertexAITrainingPipeline resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.VertexAITrainingPipelineStatus{}
	mapCtx = &direct.MapContext{}
	status.ObservedState = VertexAITrainingPipelineObservedState_FromProto(mapCtx, a.actual)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareTrainingPipeline(ctx context.Context, actual, desired *pb.TrainingPipeline) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, VertexAITrainingPipelineSpec_FromProto, VertexAITrainingPipelineSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *TrainingPipelineAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.VertexAITrainingPipeline{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(VertexAITrainingPipelineSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = a.id.Location
	obj.Spec.ResourceID = direct.LazyPtr(a.id.TrainingPipeline)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.TrainingPipeline)
	u.SetGroupVersionKind(krm.VertexAITrainingPipelineGVK)
	return u, nil
}

func (a *TrainingPipelineAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting TrainingPipeline", "name", a.id)

	req := &pb.DeleteTrainingPipelineRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteTrainingPipeline(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting TrainingPipeline %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted TrainingPipeline", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete TrainingPipeline %s: %w", a.id, err)
		}
	}
	return true, nil
}
