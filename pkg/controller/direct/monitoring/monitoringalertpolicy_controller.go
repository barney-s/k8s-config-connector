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

package monitoring

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/monitoring/v1beta1"
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

	gcp "cloud.google.com/go/monitoring/apiv3/v2"
	pb "cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.MonitoringAlertPolicyGVK, NewMonitoringAlertPolicyModel)
}

func NewMonitoringAlertPolicyModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelMonitoringAlertPolicy{config: *config}, nil
}

var _ directbase.Model = &modelMonitoringAlertPolicy{}
var _ = strings.TrimPrefix

type modelMonitoringAlertPolicy struct {
	config config.ControllerConfig
}

func (m *modelMonitoringAlertPolicy) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.MonitoringAlertPolicy{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.MonitoringAlertPolicyIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &MonitoringAlertPolicyAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelMonitoringAlertPolicy) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.MonitoringAlertPolicyIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &MonitoringAlertPolicyAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelMonitoringAlertPolicy) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building MonitoringAlertPolicy client: %w", err)
	}
	return gcpClient, err
}

type MonitoringAlertPolicyAdapter struct {
	id        *krm.MonitoringAlertPolicyIdentity
	gcpClient *gcp.Client
	desired   *krm.MonitoringAlertPolicy
	actual    *pb.MonitoringAlertPolicy
}

var _ directbase.Adapter = &MonitoringAlertPolicyAdapter{}

func (a *MonitoringAlertPolicyAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting MonitoringAlertPolicy", "name", a.id)

	req := &pb.GetMonitoringAlertPolicyRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetMonitoringAlertPolicy(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting MonitoringAlertPolicy %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *MonitoringAlertPolicyAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating MonitoringAlertPolicy", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := MonitoringAlertPolicySpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateMonitoringAlertPolicyRequest{
		Parent:                  a.id.ParentString(),
		MonitoringAlertPolicyId: a.id.HasIdentitySpecified,
		MonitoringAlertPolicy:   resource,
	}
	created, err := a.gcpClient.CreateMonitoringAlertPolicy(ctx, req)
	if err != nil {
		return fmt.Errorf("creating MonitoringAlertPolicy %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created MonitoringAlertPolicy", "name", a.id)

	status := &krm.MonitoringAlertPolicyStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *MonitoringAlertPolicyAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating MonitoringAlertPolicy", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := MonitoringAlertPolicySpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareMonitoringAlertPolicy(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("MonitoringAlertPolicy resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.MonitoringAlertPolicyStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareMonitoringAlertPolicy(ctx context.Context, actual, desired *pb.MonitoringAlertPolicy) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, MonitoringAlertPolicySpec_FromProto, MonitoringAlertPolicySpec_ToProto)
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

func (a *MonitoringAlertPolicyAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.MonitoringAlertPolicy{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(MonitoringAlertPolicySpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ResourceID = direct.LazyPtr(a.id.HasIdentitySpecified)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.HasIdentitySpecified)
	u.SetGroupVersionKind(krm.MonitoringAlertPolicyGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *MonitoringAlertPolicyAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deletion not supported for MonitoringAlertPolicy, removing finalizer without deleting GCP resource", "name", a.id)
	return true, nil
}
