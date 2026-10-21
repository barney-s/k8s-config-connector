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

package configdeliveryfleetpackage

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/configdelivery/v1alpha1"
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

	gcp "cloud.google.com/go/configdelivery/apiv1"
	pb "cloud.google.com/go/configdelivery/apiv1/configdeliverypb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.ConfigDeliveryFleetPackageGVK, NewFleetPackageModel)
}

func NewFleetPackageModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelFleetPackage{config: *config}, nil
}

var _ directbase.Model = &modelFleetPackage{}
var _ = strings.TrimPrefix

type modelFleetPackage struct {
	config config.ControllerConfig
}

func (m *modelFleetPackage) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.ConfigDeliveryFleetPackage{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.ConfigDeliveryFleetPackageIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &FleetPackageAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelFleetPackage) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.ConfigDeliveryFleetPackageIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &FleetPackageAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelFleetPackage) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building FleetPackage client: %w", err)
	}
	return gcpClient, err
}

type FleetPackageAdapter struct {
	id        *krm.ConfigDeliveryFleetPackageIdentity
	gcpClient *gcp.Client
	desired   *krm.ConfigDeliveryFleetPackage
	actual    *pb.FleetPackage
}

var _ directbase.Adapter = &FleetPackageAdapter{}

func (a *FleetPackageAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting FleetPackage", "name", a.id)

	req := &pb.GetFleetPackageRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetFleetPackage(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting FleetPackage %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *FleetPackageAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating FleetPackage", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := ConfigDeliveryFleetPackageSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateFleetPackageRequest{
		Parent:         a.id.ParentString(),
		FleetPackageId: a.id.FleetPackage,
		FleetPackage:   resource,
	}
	op, err := a.gcpClient.CreateFleetPackage(ctx, req)
	if err != nil {
		return fmt.Errorf("creating FleetPackage %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create FleetPackage %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created FleetPackage", "name", a.id)

	status := &krm.ConfigDeliveryFleetPackageStatus{}
	status.ObservedState = ConfigDeliveryFleetPackageObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *FleetPackageAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating FleetPackage", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := ConfigDeliveryFleetPackageSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareFleetPackage(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateFleetPackageRequest{
			FleetPackage: desired,
			UpdateMask:   updateMask,
		}
		op, err := a.gcpClient.UpdateFleetPackage(ctx, req)
		if err != nil {
			return fmt.Errorf("updating FleetPackage %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update FleetPackage %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated FleetPackage", "name", a.id)

		status := &krm.ConfigDeliveryFleetPackageStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = ConfigDeliveryFleetPackageObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareFleetPackage(ctx context.Context, actual, desired *pb.FleetPackage) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, ConfigDeliveryFleetPackageSpec_FromProto, ConfigDeliveryFleetPackageSpec_ToProto)
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

func (a *FleetPackageAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.ConfigDeliveryFleetPackage{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(ConfigDeliveryFleetPackageSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ResourceID = direct.LazyPtr(a.id.FleetPackage)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.FleetPackage)
	u.SetGroupVersionKind(krm.ConfigDeliveryFleetPackageGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *FleetPackageAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting FleetPackage", "name", a.id)

	req := &pb.DeleteFleetPackageRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteFleetPackage(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting FleetPackage %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted FleetPackage", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete FleetPackage %s: %w", a.id, err)
		}
	}
	return true, nil
}
