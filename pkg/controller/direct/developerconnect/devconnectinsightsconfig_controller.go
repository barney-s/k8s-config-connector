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

package developerconnect

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/developerconnect/v1alpha1"
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

	gcp "cloud.google.com/go/developerconnect/apiv1"
	pb "cloud.google.com/go/developerconnect/apiv1/developerconnectpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.DevConnectInsightsConfigGVK, NewInsightsConfigModel)
}

func NewInsightsConfigModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelInsightsConfig{config: *config}, nil
}

var _ directbase.Model = &modelInsightsConfig{}
var _ = strings.TrimPrefix

type modelInsightsConfig struct {
	config config.ControllerConfig
}

func (m *modelInsightsConfig) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.DevConnectInsightsConfig{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.DevConnectInsightsConfigIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &InsightsConfigAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelInsightsConfig) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.DevConnectInsightsConfigIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &InsightsConfigAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelInsightsConfig) client(ctx context.Context) (*gcp.InsightsConfigClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewInsightsConfigRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building InsightsConfig client: %w", err)
	}
	return gcpClient, err
}

type InsightsConfigAdapter struct {
	id        *krm.DevConnectInsightsConfigIdentity
	gcpClient *gcp.InsightsConfigClient
	desired   *krm.DevConnectInsightsConfig
	actual    *pb.InsightsConfig
}

var _ directbase.Adapter = &InsightsConfigAdapter{}

func (a *InsightsConfigAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting InsightsConfig", "name", a.id)

	req := &pb.GetInsightsConfigRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetInsightsConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting InsightsConfig %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *InsightsConfigAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating InsightsConfig", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := DevConnectInsightsConfigSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateInsightsConfigRequest{
		Parent:           a.id.ParentString(),
		InsightsConfigId: a.id.Insights,
		InsightsConfig:   resource,
	}
	op, err := a.gcpClient.CreateInsightsConfig(ctx, req)
	if err != nil {
		return fmt.Errorf("creating InsightsConfig %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create InsightsConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created InsightsConfig", "name", a.id)

	status := &krm.DevConnectInsightsConfigStatus{}
	status.ObservedState = DevConnectInsightsConfigObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *InsightsConfigAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating InsightsConfig", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := DevConnectInsightsConfigSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareInsightsConfig(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateInsightsConfigRequest{
			InsightsConfig: desired,
		}
		op, err := a.gcpClient.UpdateInsightsConfig(ctx, req)
		if err != nil {
			return fmt.Errorf("updating InsightsConfig %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update InsightsConfig %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated InsightsConfig", "name", a.id)

		status := &krm.DevConnectInsightsConfigStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = DevConnectInsightsConfigObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareInsightsConfig(ctx context.Context, actual, desired *pb.InsightsConfig) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, DevConnectInsightsConfigSpec_FromProto, DevConnectInsightsConfigSpec_ToProto)
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

func (a *InsightsConfigAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.DevConnectInsightsConfig{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(DevConnectInsightsConfigSpec_FromProto(mapCtx, a.actual))
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
	u.SetName(a.id.Insights)
	u.SetGroupVersionKind(krm.DevConnectInsightsConfigGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *InsightsConfigAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting InsightsConfig", "name", a.id)

	req := &pb.DeleteInsightsConfigRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteInsightsConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting InsightsConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted InsightsConfig", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete InsightsConfig %s: %w", a.id, err)
		}
	}
	return true, nil
}
