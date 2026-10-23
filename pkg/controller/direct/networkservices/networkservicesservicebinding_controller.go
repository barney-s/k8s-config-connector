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

package networkservices

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/networkservices/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/export"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/label"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/networkservices/apiv1"
	networkservicespb "cloud.google.com/go/networkservices/apiv1/networkservicespb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.NetworkServicesServiceBindingGVK, NewServiceBindingModel)
}

func NewServiceBindingModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelServiceBinding{config: *config}, nil
}

var _ directbase.Model = &modelServiceBinding{}
var _ = strings.TrimPrefix

type modelServiceBinding struct {
	config config.ControllerConfig
}

func (m *modelServiceBinding) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.NetworkServicesServiceBinding{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id, err := krm.NewServiceBindingIdentity(ctx, reader, obj)
	if err != nil {
		return nil, err
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ServiceBindingAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelServiceBinding) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelServiceBinding) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ServiceBinding client: %w", err)
	}
	return gcpClient, err
}

type ServiceBindingAdapter struct {
	id        *krm.ServiceBindingIdentity
	gcpClient *gcp.Client
	desired   *krm.NetworkServicesServiceBinding
	actual    *networkservicespb.ServiceBinding
}

var _ directbase.Adapter = &ServiceBindingAdapter{}

func (a *ServiceBindingAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ServiceBinding", "name", a.id)

	req := &networkservicespb.GetServiceBindingRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetServiceBinding(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ServiceBinding %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ServiceBindingAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ServiceBinding", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := NetworkServicesServiceBindingSpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &networkservicespb.CreateServiceBindingRequest{
		Parent:           a.id.Parent().String(),
		ServiceBindingId: a.id.ID(),
		ServiceBinding:   resource,
	}
	op, err := a.gcpClient.CreateServiceBinding(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ServiceBinding %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create ServiceBinding %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ServiceBinding", "name", a.id)

	status := &krm.NetworkServicesServiceBindingStatus{}
	status.ObservedState = NetworkServicesServiceBindingObservedState_v1alpha1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ServiceBindingAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ServiceBinding", "name", a.id)

	desired, diffs, updateMask, err := compareServiceBinding(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	_ = desired
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("NetworkServicesServiceBinding resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.NetworkServicesServiceBindingStatus{}
	mapCtx := &direct.MapContext{}
	status.ObservedState = NetworkServicesServiceBindingObservedState_v1alpha1_FromProto(mapCtx, a.actual)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareServiceBinding(ctx context.Context, u *unstructured.Unstructured, actual *networkservicespb.ServiceBinding, desiredSpec *krm.NetworkServicesServiceBindingSpec) (*networkservicespb.ServiceBinding, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	return common.CompareGreenfieldSpecAndLabels(
		ctx,
		u,
		desiredSpec,
		actual,
		"labels",
		NetworkServicesServiceBindingSpec_v1alpha1_FromProto,
		NetworkServicesServiceBindingSpec_v1alpha1_ToProto,
		nil,
	)
}

func (a *ServiceBindingAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.NetworkServicesServiceBinding{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(NetworkServicesServiceBindingSpec_v1alpha1_FromProto(mapCtx, a.actual))
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
	u.SetGroupVersionKind(krm.NetworkServicesServiceBindingGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *ServiceBindingAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting ServiceBinding", "name", a.id)

	req := &networkservicespb.DeleteServiceBindingRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteServiceBinding(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting ServiceBinding %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted ServiceBinding", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete ServiceBinding %s: %w", a.id, err)
		}
	}
	return true, nil
}
