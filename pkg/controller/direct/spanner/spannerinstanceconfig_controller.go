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

package spanner

import (
	"context"
	"fmt"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/spanner/v1alpha1"
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

	gcp "cloud.google.com/go/spanner/admin/instance/apiv1"
	instancepb "cloud.google.com/go/spanner/admin/instance/apiv1/instancepb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.SpannerInstanceConfigGVK, NewInstanceConfigModel)
}

func NewInstanceConfigModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelInstanceConfig{config: *config}, nil
}

var _ directbase.Model = &modelInstanceConfig{}
var _ = strings.TrimPrefix

type modelInstanceConfig struct {
	config config.ControllerConfig
}

func (m *modelInstanceConfig) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.SpannerInstanceConfig{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.InstanceConfigIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &InstanceConfigAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelInstanceConfig) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelInstanceConfig) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building InstanceConfig client: %w", err)
	}
	return gcpClient, err
}

type InstanceConfigAdapter struct {
	id        *krm.InstanceConfigIdentity
	gcpClient *gcp.Client
	desired   *krm.SpannerInstanceConfig
	actual    *instancepb.InstanceConfig
}

var _ directbase.Adapter = &InstanceConfigAdapter{}

func (a *InstanceConfigAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting InstanceConfig", "name", a.id)

	req := &instancepb.GetInstanceConfigRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetInstanceConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting InstanceConfig %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *InstanceConfigAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating InstanceConfig", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := SpannerInstanceConfigSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &instancepb.CreateInstanceConfigRequest{
		Parent:           a.id.Parent().String(),
		InstanceConfigId: a.id.ID(),
		InstanceConfig:   resource,
	}
	op, err := a.gcpClient.CreateInstanceConfig(ctx, req)
	if err != nil {
		return fmt.Errorf("creating InstanceConfig %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create InstanceConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created InstanceConfig", "name", a.id)

	status := &krm.SpannerInstanceConfigStatus{}
	status.ObservedState = SpannerInstanceConfigObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *InstanceConfigAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating InstanceConfig", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := SpannerInstanceConfigSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareInstanceConfig(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &instancepb.UpdateInstanceConfigRequest{
			InstanceConfig: desired,
			UpdateMask:     updateMask,
		}
		op, err := a.gcpClient.UpdateInstanceConfig(ctx, req)
		if err != nil {
			return fmt.Errorf("updating InstanceConfig %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update InstanceConfig %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated InstanceConfig", "name", a.id)

		status := &krm.SpannerInstanceConfigStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = SpannerInstanceConfigObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareInstanceConfig(ctx context.Context, actual, desired *instancepb.InstanceConfig) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, SpannerInstanceConfigSpec_FromProto, SpannerInstanceConfigSpec_ToProto)
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

func (a *InstanceConfigAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.SpannerInstanceConfig{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(SpannerInstanceConfigSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.SpannerInstanceConfigGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *InstanceConfigAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting InstanceConfig", "name", a.id)

	req := &instancepb.DeleteInstanceConfigRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteInstanceConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting InstanceConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted InstanceConfig", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete InstanceConfig %s: %w", a.id, err)
		}
	}
	return true, nil
}
