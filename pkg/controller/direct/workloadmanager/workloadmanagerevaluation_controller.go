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

package workloadmanager

import (
	"context"
	"fmt"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/workloadmanager/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/workloadmanager/apiv1"
	workloadmanagerpb "cloud.google.com/go/workloadmanager/apiv1/workloadmanagerpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.WorkloadManagerEvaluationGVK, NewEvaluationModel)
}

func NewEvaluationModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelEvaluation{config: *config}, nil
}

var _ directbase.Model = &modelEvaluation{}
var _ = strings.TrimPrefix

type modelEvaluation struct {
	config config.ControllerConfig
}

func (m *modelEvaluation) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.WorkloadManagerEvaluation{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.WorkloadManagerEvaluationIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &EvaluationAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelEvaluation) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelEvaluation) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Evaluation client: %w", err)
	}
	return gcpClient, err
}

type EvaluationAdapter struct {
	id        *krm.WorkloadManagerEvaluationIdentity
	gcpClient *gcp.Client
	desired   *krm.WorkloadManagerEvaluation
	actual    *workloadmanagerpb.Evaluation
}

var _ directbase.Adapter = &EvaluationAdapter{}

func (a *EvaluationAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Evaluation", "name", a.id)

	req := &workloadmanagerpb.GetEvaluationRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetEvaluation(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Evaluation %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *EvaluationAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Evaluation", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := WorkloadManagerEvaluationSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &workloadmanagerpb.CreateEvaluationRequest{
		Parent:       a.id.ParentString(),
		EvaluationId: a.id.Evaluation,
		Evaluation:   resource,
	}
	op, err := a.gcpClient.CreateEvaluation(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Evaluation %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create Evaluation %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Evaluation", "name", a.id)

	status := &krm.WorkloadManagerEvaluationStatus{}
	status.ObservedState = WorkloadManagerEvaluationObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *EvaluationAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Evaluation", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := WorkloadManagerEvaluationSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareEvaluation(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &workloadmanagerpb.UpdateEvaluationRequest{
			Evaluation: desired,
			UpdateMask: updateMask,
		}
		op, err := a.gcpClient.UpdateEvaluation(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Evaluation %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update Evaluation %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Evaluation", "name", a.id)

		status := &krm.WorkloadManagerEvaluationStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = WorkloadManagerEvaluationObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareEvaluation(ctx context.Context, actual, desired *workloadmanagerpb.Evaluation) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, WorkloadManagerEvaluationSpec_FromProto, WorkloadManagerEvaluationSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *EvaluationAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.WorkloadManagerEvaluation{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(WorkloadManagerEvaluationSpec_FromProto(mapCtx, a.actual))
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
	u.SetName(a.id.Evaluation)
	u.SetGroupVersionKind(krm.WorkloadManagerEvaluationGVK)
	return u, nil
}

func (a *EvaluationAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Evaluation", "name", a.id)

	req := &workloadmanagerpb.DeleteEvaluationRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteEvaluation(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Evaluation %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Evaluation", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete Evaluation %s: %w", a.id, err)
		}
	}
	return true, nil
}
