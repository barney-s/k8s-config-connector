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
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/networkservices/apiv1"
	pb "cloud.google.com/go/networkservices/apiv1/networkservicespb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.NetworkServicesServiceLBPolicyGVK, NewServiceLbPolicyModel)
}

func NewServiceLbPolicyModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelServiceLbPolicy{config: *config}, nil
}

var _ directbase.Model = &modelServiceLbPolicy{}
var _ = strings.TrimPrefix

type modelServiceLbPolicy struct {
	config config.ControllerConfig
}

func (m *modelServiceLbPolicy) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.NetworkServicesServiceLBPolicy{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.NetworkServicesServiceLBPolicyIdentity{}
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ServiceLbPolicyAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelServiceLbPolicy) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelServiceLbPolicy) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ServiceLbPolicy client: %w", err)
	}
	return gcpClient, err
}

type ServiceLbPolicyAdapter struct {
	id        *krm.NetworkServicesServiceLBPolicyIdentity
	gcpClient *gcp.Client
	desired   *krm.NetworkServicesServiceLBPolicy
	actual    *pb.ServiceLbPolicy
}

var _ directbase.Adapter = &ServiceLbPolicyAdapter{}

func (a *ServiceLbPolicyAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ServiceLbPolicy", "name", a.id)

	req := &pb.GetServiceLbPolicyRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetServiceLbPolicy(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ServiceLbPolicy %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ServiceLbPolicyAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ServiceLbPolicy", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := NetworkServicesServiceLBPolicySpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateServiceLbPolicyRequest{
		Parent:            a.id.ParentString(),
		ServiceLbPolicyId: a.id.ServiceLbPolicy,
		ServiceLbPolicy:   resource,
	}
	op, err := a.gcpClient.CreateServiceLbPolicy(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ServiceLbPolicy %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create ServiceLbPolicy %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ServiceLbPolicy", "name", a.id)

	status := &krm.NetworkServicesServiceLBPolicyStatus{}
	status.ObservedState = NetworkServicesServiceLBPolicyObservedState_v1alpha1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ServiceLbPolicyAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ServiceLbPolicy", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := NetworkServicesServiceLBPolicySpec_v1alpha1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareServiceLbPolicy(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateServiceLbPolicyRequest{
			ServiceLbPolicy: desired,
			UpdateMask:      updateMask,
		}
		op, err := a.gcpClient.UpdateServiceLbPolicy(ctx, req)
		if err != nil {
			return fmt.Errorf("updating ServiceLbPolicy %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update ServiceLbPolicy %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated ServiceLbPolicy", "name", a.id)

		status := &krm.NetworkServicesServiceLBPolicyStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = NetworkServicesServiceLBPolicyObservedState_v1alpha1_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareServiceLbPolicy(ctx context.Context, actual, desired *pb.ServiceLbPolicy) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, NetworkServicesServiceLBPolicySpec_v1alpha1_FromProto, NetworkServicesServiceLBPolicySpec_v1alpha1_ToProto)
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

func (a *ServiceLbPolicyAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.NetworkServicesServiceLBPolicy{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(NetworkServicesServiceLBPolicySpec_v1alpha1_FromProto(mapCtx, a.actual))
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
	u.SetName(a.id.ServiceLbPolicy)
	u.SetGroupVersionKind(krm.NetworkServicesServiceLBPolicyGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *ServiceLbPolicyAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting ServiceLbPolicy", "name", a.id)

	req := &pb.DeleteServiceLbPolicyRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteServiceLbPolicy(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting ServiceLbPolicy %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted ServiceLbPolicy", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete ServiceLbPolicy %s: %w", a.id, err)
		}
	}
	return true, nil
}
