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

package eventarc

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/eventarc/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/eventarc/apiv1"
	pb "cloud.google.com/go/eventarc/apiv1/eventarcpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.EventarcPipelineGVK, NewPipelineModel)
}

func NewPipelineModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelPipeline{config: *config}, nil
}

var _ directbase.Model = &modelPipeline{}
var _ = strings.TrimPrefix

type modelPipeline struct {
	config config.ControllerConfig
}

func (m *modelPipeline) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.EventarcPipeline{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.EventarcPipelineIdentity{}
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &PipelineAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelPipeline) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.EventarcPipelineIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &PipelineAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelPipeline) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Pipeline client: %w", err)
	}
	return gcpClient, err
}

type PipelineAdapter struct {
	id        *krm.EventarcPipelineIdentity
	gcpClient *gcp.Client
	desired   *krm.EventarcPipeline
	actual    *pb.Pipeline
}

var _ directbase.Adapter = &PipelineAdapter{}

func (a *PipelineAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Pipeline", "name", a.id)

	req := &pb.GetPipelineRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetPipeline(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Pipeline %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *PipelineAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Pipeline", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := EventarcPipelineSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreatePipelineRequest{
		Parent:     a.id.ParentString(),
		PipelineId: a.id.Pipeline,
		Pipeline:   resource,
	}
	op, err := a.gcpClient.CreatePipeline(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Pipeline %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create Pipeline %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Pipeline", "name", a.id)

	status := &krm.EventarcPipelineStatus{}
	status.ObservedState = EventarcPipelineObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *PipelineAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Pipeline", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := EventarcPipelineSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := comparePipeline(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdatePipelineRequest{
			Pipeline:   desired,
			UpdateMask: updateMask,
		}
		op, err := a.gcpClient.UpdatePipeline(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Pipeline %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update Pipeline %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Pipeline", "name", a.id)

		status := &krm.EventarcPipelineStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = EventarcPipelineObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func comparePipeline(ctx context.Context, actual, desired *pb.Pipeline) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, EventarcPipelineSpec_FromProto, EventarcPipelineSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *PipelineAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.EventarcPipeline{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(EventarcPipelineSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = a.id.Location
	obj.Spec.ResourceID = direct.LazyPtr(a.id.Pipeline)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Pipeline)
	u.SetGroupVersionKind(krm.EventarcPipelineGVK)
	return u, nil
}

func (a *PipelineAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Pipeline", "name", a.id)

	req := &pb.DeletePipelineRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeletePipeline(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Pipeline %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Pipeline", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete Pipeline %s: %w", a.id, err)
		}
	}
	return true, nil
}
