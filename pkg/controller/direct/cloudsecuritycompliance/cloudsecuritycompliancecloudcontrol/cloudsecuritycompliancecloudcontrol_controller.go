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

package cloudsecuritycompliancecloudcontrol

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/cloudsecuritycompliance/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/cloudsecuritycompliance/apiv1"
	pb "cloud.google.com/go/cloudsecuritycompliance/apiv1/cloudsecuritycompliancepb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.CloudSecurityComplianceCloudControlGVK, NewCloudControlModel)
}

func NewCloudControlModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelCloudControl{config: *config}, nil
}

var _ directbase.Model = &modelCloudControl{}
var _ = strings.TrimPrefix

type modelCloudControl struct {
	config config.ControllerConfig
}

func (m *modelCloudControl) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.CloudSecurityComplianceCloudControl{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.CloudSecurityComplianceCloudControlIdentity)

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &CloudControlAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelCloudControl) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelCloudControl) client(ctx context.Context) (*gcp.ConfigClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewConfigRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building CloudControl client: %w", err)
	}
	return gcpClient, err
}

type CloudControlAdapter struct {
	id        *krm.CloudSecurityComplianceCloudControlIdentity
	gcpClient *gcp.ConfigClient
	desired   *krm.CloudSecurityComplianceCloudControl
	actual    *pb.CloudControl
}

var _ directbase.Adapter = &CloudControlAdapter{}

func (a *CloudControlAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting CloudControl", "name", a.id)

	req := &pb.GetCloudControlRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetCloudControl(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting CloudControl %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *CloudControlAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating CloudControl", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := CloudSecurityComplianceCloudControlSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateCloudControlRequest{
		Parent:         a.id.ParentString(),
		CloudControlId: a.id.CloudControl,
		CloudControl:   resource,
	}
	created, err := a.gcpClient.CreateCloudControl(ctx, req)
	if err != nil {
		return fmt.Errorf("creating CloudControl %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created CloudControl", "name", a.id)

	status := &krm.CloudSecurityComplianceCloudControlStatus{}
	status.ObservedState = CloudSecurityComplianceCloudControlObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *CloudControlAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating CloudControl", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := CloudSecurityComplianceCloudControlSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareCloudControl(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateCloudControlRequest{
			CloudControl: desired,
			UpdateMask:   updateMask,
		}
		updated, err := a.gcpClient.UpdateCloudControl(ctx, req)
		if err != nil {
			return fmt.Errorf("updating CloudControl %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated CloudControl", "name", a.id)

		status := &krm.CloudSecurityComplianceCloudControlStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = CloudSecurityComplianceCloudControlObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareCloudControl(ctx context.Context, actual, desired *pb.CloudControl) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, CloudSecurityComplianceCloudControlSpec_FromProto, CloudSecurityComplianceCloudControlSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *CloudControlAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.CloudSecurityComplianceCloudControl{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(CloudSecurityComplianceCloudControlSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.CloudControl)
	u.SetGroupVersionKind(krm.CloudSecurityComplianceCloudControlGVK)
	return u, nil
}

func (a *CloudControlAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting CloudControl", "name", a.id)

	req := &pb.DeleteCloudControlRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteCloudControl(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting CloudControl %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted CloudControl", "name", a.id)
	return true, nil
}
