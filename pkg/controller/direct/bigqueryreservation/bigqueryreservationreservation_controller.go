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
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
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
	registry.RegisterModel(krm.BigQueryReservationReservationGVK, NewReservationModel)
}

func NewReservationModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelReservation{config: *config}, nil
}

var _ directbase.Model = &modelReservation{}
var _ = strings.TrimPrefix

type modelReservation struct {
	config config.ControllerConfig
}

func (m *modelReservation) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.BigQueryReservationReservation{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id, err := krm.NewReservationIdentity(ctx, reader, obj)
	if err != nil {
		return nil, err
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ReservationAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelReservation) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelReservation) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Reservation client: %w", err)
	}
	return gcpClient, err
}

type ReservationAdapter struct {
	id        *krm.ReservationIdentity
	gcpClient *gcp.Client
	desired   *krm.BigQueryReservationReservation
	actual    *pb.Reservation
}

var _ directbase.Adapter = &ReservationAdapter{}

func (a *ReservationAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Reservation", "name", a.id)

	req := &pb.GetReservationRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetReservation(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Reservation %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ReservationAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Reservation", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := BigQueryReservationReservationSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateReservationRequest{
		Parent:        a.id.Parent().String(),
		ReservationId: a.id.ID(),
		Reservation:   resource,
	}
	created, err := a.gcpClient.CreateReservation(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Reservation %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Reservation", "name", a.id)

	status := &krm.BigQueryReservationReservationStatus{}
	status.ObservedState = BigQueryReservationReservationObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ReservationAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Reservation", "name", a.id)

	desired, diffs, updateMask, err := compareReservation(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateReservationRequest{
			Reservation: desired,
			UpdateMask:  updateMask,
		}
		updated, err := a.gcpClient.UpdateReservation(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Reservation %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Reservation", "name", a.id)

		status := &krm.BigQueryReservationReservationStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = BigQueryReservationReservationObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareReservation(ctx context.Context, u *unstructured.Unstructured, actual *pb.Reservation, desiredSpec *krm.BigQueryReservationReservationSpec) (*pb.Reservation, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		BigQueryReservationReservationSpec_FromProto,
		BigQueryReservationReservationSpec_ToProto,
		nil,
	)
}

func (a *ReservationAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.BigQueryReservationReservation{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(BigQueryReservationReservationSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Parent().ProjectID}
	obj.Spec.Location = a.id.Parent().Location

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.BigQueryReservationReservationGVK)
	return u, nil
}

func (a *ReservationAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Reservation", "name", a.id)

	req := &pb.DeleteReservationRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteReservation(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Reservation %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Reservation", "name", a.id)
	return true, nil
}
