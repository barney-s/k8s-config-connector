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
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/export"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/label"
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
	registry.RegisterModel(krm.AIPlatformReasoningEngineGVK, NewReasoningEngineModel)
}

func NewReasoningEngineModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelReasoningEngine{config: *config}, nil
}

var _ directbase.Model = &modelReasoningEngine{}
var _ = strings.TrimPrefix

type modelReasoningEngine struct {
	config config.ControllerConfig
}

func (m *modelReasoningEngine) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.AIPlatformReasoningEngine{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.AIPlatformReasoningEngineIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ReasoningEngineAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelReasoningEngine) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.AIPlatformReasoningEngineIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ReasoningEngineAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelReasoningEngine) client(ctx context.Context) (*gcp.ReasoningEngineClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.GRPCClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewReasoningEngineClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ReasoningEngine client: %w", err)
	}
	return gcpClient, err
}

type ReasoningEngineAdapter struct {
	id        *krm.AIPlatformReasoningEngineIdentity
	gcpClient *gcp.ReasoningEngineClient
	desired   *krm.AIPlatformReasoningEngine
	actual    *pb.ReasoningEngine
}

var _ directbase.Adapter = &ReasoningEngineAdapter{}

func (a *ReasoningEngineAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ReasoningEngine", "name", a.id)

	req := &pb.GetReasoningEngineRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetReasoningEngine(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ReasoningEngine %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ReasoningEngineAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ReasoningEngine", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := AIPlatformReasoningEngineSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateReasoningEngineRequest{
		Parent:            a.id.ParentString(),
		ReasoningEngineId: a.id.ReasoningEngine,
		ReasoningEngine:   resource,
	}
	op, err := a.gcpClient.CreateReasoningEngine(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ReasoningEngine %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create ReasoningEngine %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ReasoningEngine", "name", a.id)

	status := &krm.AIPlatformReasoningEngineStatus{}
	status.ObservedState = AIPlatformReasoningEngineObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ReasoningEngineAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ReasoningEngine", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := AIPlatformReasoningEngineSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareReasoningEngine(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateReasoningEngineRequest{
			ReasoningEngine: desired,
			UpdateMask:      updateMask,
		}
		op, err := a.gcpClient.UpdateReasoningEngine(ctx, req)
		if err != nil {
			return fmt.Errorf("updating ReasoningEngine %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update ReasoningEngine %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated ReasoningEngine", "name", a.id)

		status := &krm.AIPlatformReasoningEngineStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = AIPlatformReasoningEngineObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareReasoningEngine(ctx context.Context, actual, desired *pb.ReasoningEngine) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, AIPlatformReasoningEngineSpec_FromProto, AIPlatformReasoningEngineSpec_ToProto)
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

func (a *ReasoningEngineAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.AIPlatformReasoningEngine{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(AIPlatformReasoningEngineSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = a.id.Location
	obj.Spec.ResourceID = direct.LazyPtr(a.id.ReasoningEngine)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ReasoningEngine)
	u.SetGroupVersionKind(krm.AIPlatformReasoningEngineGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *ReasoningEngineAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting ReasoningEngine", "name", a.id)

	req := &pb.DeleteReasoningEngineRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteReasoningEngine(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting ReasoningEngine %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted ReasoningEngine", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete ReasoningEngine %s: %w", a.id, err)
		}
	}
	return true, nil
}
