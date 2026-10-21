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

package servicedirectory

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/servicedirectory/v1beta1"
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

	gcp "cloud.google.com/go/servicedirectory/apiv1beta1"
	pb "cloud.google.com/go/servicedirectory/apiv1beta1/servicedirectorypb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.ServiceDirectoryServiceGVK, NewServiceModel)
}

func NewServiceModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelService{config: *config}, nil
}

var _ directbase.Model = &modelService{}
var _ = strings.TrimPrefix

type modelService struct {
	config config.ControllerConfig
}

func (m *modelService) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.ServiceDirectoryService{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.ServiceDirectoryServiceIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ServiceAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelService) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelService) client(ctx context.Context) (*gcp.RegistrationClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRegistrationRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Service client: %w", err)
	}
	return gcpClient, err
}

type ServiceAdapter struct {
	id        *krm.ServiceDirectoryServiceIdentity
	gcpClient *gcp.RegistrationClient
	desired   *krm.ServiceDirectoryService
	actual    *pb.Service
}

var _ directbase.Adapter = &ServiceAdapter{}

func (a *ServiceAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Service", "name", a.id)

	req := &pb.GetServiceRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetService(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Service %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ServiceAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Service", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := ServiceDirectoryServiceSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateServiceRequest{
		Parent:    a.id.ParentString(),
		ServiceId: a.id.Service,
		Service:   resource,
	}
	created, err := a.gcpClient.CreateService(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Service %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Service", "name", a.id)

	status := &krm.ServiceDirectoryServiceStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ServiceAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Service", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := ServiceDirectoryServiceSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareService(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateServiceRequest{
			Service:    desired,
			UpdateMask: updateMask,
		}
		updated, err := a.gcpClient.UpdateService(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Service %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Service", "name", a.id)

		status := &krm.ServiceDirectoryServiceStatus{}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareService(ctx context.Context, actual, desired *pb.Service) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, ServiceDirectoryServiceSpec_FromProto, ServiceDirectoryServiceSpec_ToProto)
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

func (a *ServiceAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.ServiceDirectoryService{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(ServiceDirectoryServiceSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Service)
	u.SetGroupVersionKind(krm.ServiceDirectoryServiceGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *ServiceAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Service", "name", a.id)

	req := &pb.DeleteServiceRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteService(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Service %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Service", "name", a.id)
	return true, nil
}
