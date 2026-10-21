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

package bigqueryreservation

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/bigqueryreservation/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/bigquery/reservation/apiv1"
	pb "cloud.google.com/go/bigquery/reservation/apiv1/reservationpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.BigQueryReservationAssignmentGVK, NewBigQueryReservationAssignmentModel)
}

func NewBigQueryReservationAssignmentModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelBigQueryReservationAssignment{config: *config}, nil
}

var _ directbase.Model = &modelBigQueryReservationAssignment{}
var _ = strings.TrimPrefix

type modelBigQueryReservationAssignment struct {
	config config.ControllerConfig
}

func (m *modelBigQueryReservationAssignment) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.BigQueryReservationAssignment{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.AssignmentIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &BigQueryReservationAssignmentAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelBigQueryReservationAssignment) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelBigQueryReservationAssignment) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building BigQueryReservationAssignment client: %w", err)
	}
	return gcpClient, err
}

type BigQueryReservationAssignmentAdapter struct {
	id        *krm.AssignmentIdentity
	gcpClient *gcp.Client
	desired   *krm.BigQueryReservationAssignment
	actual    *pb.BigQueryReservationAssignment
}

var _ directbase.Adapter = &BigQueryReservationAssignmentAdapter{}

func (a *BigQueryReservationAssignmentAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting BigQueryReservationAssignment", "name", a.id)

	req := &pb.GetBigQueryReservationAssignmentRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetBigQueryReservationAssignment(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting BigQueryReservationAssignment %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *BigQueryReservationAssignmentAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating BigQueryReservationAssignment", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := BigQueryReservationAssignmentSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateBigQueryReservationAssignmentRequest{
		Parent:                        a.id.Parent().String(),
		AssignmentId:                  a.id.AssignmentID,
		BigQueryReservationAssignment: resource,
	}
	created, err := a.gcpClient.CreateBigQueryReservationAssignment(ctx, req)
	if err != nil {
		return fmt.Errorf("creating BigQueryReservationAssignment %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created BigQueryReservationAssignment", "name", a.id)

	status := &krm.BigQueryReservationAssignmentStatus{}
	status.ObservedState = BigQueryReservationAssignmentObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *BigQueryReservationAssignmentAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating BigQueryReservationAssignment", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := BigQueryReservationAssignmentSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareBigQueryReservationAssignment(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("BigQueryReservationAssignment resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.BigQueryReservationAssignmentStatus{}
	mapCtx = &direct.MapContext{}
	status.ObservedState = BigQueryReservationAssignmentObservedState_FromProto(mapCtx, a.actual)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareBigQueryReservationAssignment(ctx context.Context, actual, desired *pb.BigQueryReservationAssignment) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, BigQueryReservationAssignmentSpec_FromProto, BigQueryReservationAssignmentSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *BigQueryReservationAssignmentAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.BigQueryReservationAssignment{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(BigQueryReservationAssignmentSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.AssignmentID)
	u.SetGroupVersionKind(krm.BigQueryReservationAssignmentGVK)
	return u, nil
}

func (a *BigQueryReservationAssignmentAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deletion not supported for BigQueryReservationAssignment, removing finalizer without deleting GCP resource", "name", a.id)
	return true, nil
}
