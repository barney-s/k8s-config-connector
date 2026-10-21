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
	registry.RegisterModel(krm.ServiceDirectoryEndpointGVK, NewEndpointModel)
}

func NewEndpointModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelEndpoint{config: *config}, nil
}

var _ directbase.Model = &modelEndpoint{}
var _ = strings.TrimPrefix

type modelEndpoint struct {
	config config.ControllerConfig
}

func (m *modelEndpoint) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.ServiceDirectoryEndpoint{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.ServiceDirectoryEndpointIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &EndpointAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelEndpoint) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelEndpoint) client(ctx context.Context) (*gcp.RegistrationClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRegistrationRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Endpoint client: %w", err)
	}
	return gcpClient, err
}

type EndpointAdapter struct {
	id        *krm.ServiceDirectoryEndpointIdentity
	gcpClient *gcp.RegistrationClient
	desired   *krm.ServiceDirectoryEndpoint
	actual    *pb.Endpoint
}

var _ directbase.Adapter = &EndpointAdapter{}

func (a *EndpointAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Endpoint", "name", a.id)

	req := &pb.GetEndpointRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetEndpoint(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Endpoint %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *EndpointAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Endpoint", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := ServiceDirectoryEndpointSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateEndpointRequest{
		Parent:     a.id.ParentString(),
		EndpointId: a.id.Endpoint,
		Endpoint:   resource,
	}
	created, err := a.gcpClient.CreateEndpoint(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Endpoint %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Endpoint", "name", a.id)

	status := &krm.ServiceDirectoryEndpointStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *EndpointAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Endpoint", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := ServiceDirectoryEndpointSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareEndpoint(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateEndpointRequest{
			Endpoint:   desired,
			UpdateMask: updateMask,
		}
		updated, err := a.gcpClient.UpdateEndpoint(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Endpoint %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Endpoint", "name", a.id)

		status := &krm.ServiceDirectoryEndpointStatus{}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareEndpoint(ctx context.Context, actual, desired *pb.Endpoint) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, ServiceDirectoryEndpointSpec_FromProto, ServiceDirectoryEndpointSpec_ToProto)
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

func (a *EndpointAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.ServiceDirectoryEndpoint{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(ServiceDirectoryEndpointSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Endpoint)
	u.SetGroupVersionKind(krm.ServiceDirectoryEndpointGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *EndpointAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Endpoint", "name", a.id)

	req := &pb.DeleteEndpointRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteEndpoint(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Endpoint %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Endpoint", "name", a.id)
	return true, nil
}
