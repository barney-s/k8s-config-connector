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

package securitycentermanagement

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/securitycentermanagement/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/securitycentermanagement/apiv1"
	securitycentermanagementpb "cloud.google.com/go/securitycentermanagement/apiv1/securitycentermanagementpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.SecurityCenterManagementEventThreatDetectionCustomModuleGVK, NewEventThreatDetectionCustomModuleModel)
}

func NewEventThreatDetectionCustomModuleModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelEventThreatDetectionCustomModule{config: *config}, nil
}

var _ directbase.Model = &modelEventThreatDetectionCustomModule{}
var _ = strings.TrimPrefix

type modelEventThreatDetectionCustomModule struct {
	config config.ControllerConfig
}

func (m *modelEventThreatDetectionCustomModule) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.SecurityCenterManagementEventThreatDetectionCustomModule{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.SecurityCenterManagementEventThreatDetectionCustomModuleIdentity)

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &EventThreatDetectionCustomModuleAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelEventThreatDetectionCustomModule) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelEventThreatDetectionCustomModule) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building EventThreatDetectionCustomModule client: %w", err)
	}
	return gcpClient, err
}

type EventThreatDetectionCustomModuleAdapter struct {
	id        *krm.SecurityCenterManagementEventThreatDetectionCustomModuleIdentity
	gcpClient *gcp.Client
	desired   *krm.SecurityCenterManagementEventThreatDetectionCustomModule
	actual    *securitycentermanagementpb.EventThreatDetectionCustomModule
}

var _ directbase.Adapter = &EventThreatDetectionCustomModuleAdapter{}

func (a *EventThreatDetectionCustomModuleAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting EventThreatDetectionCustomModule", "name", a.id)
	if a.id.Organization == "" {
		return false, nil
	}

	req := &securitycentermanagementpb.GetEventThreatDetectionCustomModuleRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetEventThreatDetectionCustomModule(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting EventThreatDetectionCustomModule %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *EventThreatDetectionCustomModuleAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating EventThreatDetectionCustomModule", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := SecurityCenterManagementEventThreatDetectionCustomModuleSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &securitycentermanagementpb.CreateEventThreatDetectionCustomModuleRequest{
		Parent:                           a.id.ParentString(),
		EventThreatDetectionCustomModule: resource,
	}
	created, err := a.gcpClient.CreateEventThreatDetectionCustomModule(ctx, req)
	if err != nil {
		return fmt.Errorf("creating EventThreatDetectionCustomModule %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created EventThreatDetectionCustomModule", "name", a.id)

	status := &krm.SecurityCenterManagementEventThreatDetectionCustomModuleStatus{}
	status.ObservedState = SecurityCenterManagementEventThreatDetectionCustomModuleObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(created.GetName())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *EventThreatDetectionCustomModuleAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating EventThreatDetectionCustomModule", "name", a.id)

	desired, diffs, updateMask, err := compareEventThreatDetectionCustomModule(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &securitycentermanagementpb.UpdateEventThreatDetectionCustomModuleRequest{
			EventThreatDetectionCustomModule: desired,
			UpdateMask:                       updateMask,
		}
		updated, err := a.gcpClient.UpdateEventThreatDetectionCustomModule(ctx, req)
		if err != nil {
			return fmt.Errorf("updating EventThreatDetectionCustomModule %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated EventThreatDetectionCustomModule", "name", a.id)

		status := &krm.SecurityCenterManagementEventThreatDetectionCustomModuleStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = SecurityCenterManagementEventThreatDetectionCustomModuleObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareEventThreatDetectionCustomModule(ctx context.Context, u *unstructured.Unstructured, actual *securitycentermanagementpb.EventThreatDetectionCustomModule, desiredSpec *krm.SecurityCenterManagementEventThreatDetectionCustomModuleSpec) (*securitycentermanagementpb.EventThreatDetectionCustomModule, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		SecurityCenterManagementEventThreatDetectionCustomModuleSpec_FromProto,
		SecurityCenterManagementEventThreatDetectionCustomModuleSpec_ToProto,
		nil,
	)
}

func (a *EventThreatDetectionCustomModuleAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.SecurityCenterManagementEventThreatDetectionCustomModule{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(SecurityCenterManagementEventThreatDetectionCustomModuleSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.Location = &a.id.Location

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Organization)
	u.SetGroupVersionKind(krm.SecurityCenterManagementEventThreatDetectionCustomModuleGVK)
	return u, nil
}

func (a *EventThreatDetectionCustomModuleAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting EventThreatDetectionCustomModule", "name", a.id)

	req := &securitycentermanagementpb.DeleteEventThreatDetectionCustomModuleRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteEventThreatDetectionCustomModule(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting EventThreatDetectionCustomModule %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted EventThreatDetectionCustomModule", "name", a.id)
	return true, nil
}
