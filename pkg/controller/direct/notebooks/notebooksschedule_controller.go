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
	registry.RegisterModel(krm.NotebooksScheduleGVK, NewScheduleModel)
}

func NewScheduleModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelSchedule{config: *config}, nil
}

var _ directbase.Model = &modelSchedule{}
var _ = strings.TrimPrefix

type modelSchedule struct {
	config config.ControllerConfig
}

func (m *modelSchedule) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.NotebooksSchedule{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id, err := krm.NewScheduleIdentity(ctx, reader, obj)
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
	return &ScheduleAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelSchedule) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.NotebooksScheduleIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ScheduleAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelSchedule) client(ctx context.Context) (*gcp.NotebookClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.GRPCClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewNotebookClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Schedule client: %w", err)
	}
	return gcpClient, err
}

type ScheduleAdapter struct {
	id        *krm.NotebooksScheduleIdentity
	gcpClient *gcp.NotebookClient
	desired   *krm.NotebooksSchedule
	actual    *pb.Schedule
}

var _ directbase.Adapter = &ScheduleAdapter{}

func (a *ScheduleAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Schedule", "name", a.id)

	req := &pb.GetScheduleRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetSchedule(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Schedule %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ScheduleAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Schedule", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := NotebooksScheduleSpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateScheduleRequest{
		Parent:     a.id.ParentString(),
		ScheduleId: a.id.ID(),
		Schedule:   resource,
	}
	op, err := a.gcpClient.CreateSchedule(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Schedule %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create Schedule %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Schedule", "name", a.id)

	status := &krm.NotebooksScheduleStatus{}
	status.ObservedState = NotebooksScheduleObservedState_v1alpha1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ScheduleAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Schedule", "name", a.id)

	desired, diffs, updateMask, err := compareSchedule(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	_ = desired
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("NotebooksSchedule resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.NotebooksScheduleStatus{}
	mapCtx := &direct.MapContext{}
	status.ObservedState = NotebooksScheduleObservedState_v1alpha1_FromProto(mapCtx, a.actual)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareSchedule(ctx context.Context, u *unstructured.Unstructured, actual *pb.Schedule, desiredSpec *krm.NotebooksScheduleSpec) (*pb.Schedule, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		NotebooksScheduleSpec_v1alpha1_FromProto,
		NotebooksScheduleSpec_v1alpha1_ToProto,
		nil,
	)
}

func (a *ScheduleAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.NotebooksSchedule{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(NotebooksScheduleSpec_v1alpha1_FromProto(mapCtx, a.actual))
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
	u.SetGroupVersionKind(krm.NotebooksScheduleGVK)
	return u, nil
}

func (a *ScheduleAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Schedule", "name", a.id)

	req := &pb.DeleteScheduleRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteSchedule(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Schedule %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Schedule", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete Schedule %s: %w", a.id, err)
		}
	}
	return true, nil
}
