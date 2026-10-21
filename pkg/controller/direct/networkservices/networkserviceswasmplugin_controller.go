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
	networkservicespb "cloud.google.com/go/networkservices/apiv1/networkservicespb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.NetworkServicesWasmPluginGVK, NewWasmPluginModel)
}

func NewWasmPluginModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelWasmPlugin{config: *config}, nil
}

var _ directbase.Model = &modelWasmPlugin{}
var _ = strings.TrimPrefix

type modelWasmPlugin struct {
	config config.ControllerConfig
}

func (m *modelWasmPlugin) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.NetworkServicesWasmPlugin{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.WasmPluginIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &WasmPluginAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelWasmPlugin) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelWasmPlugin) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building WasmPlugin client: %w", err)
	}
	return gcpClient, err
}

type WasmPluginAdapter struct {
	id        *krm.WasmPluginIdentity
	gcpClient *gcp.Client
	desired   *krm.NetworkServicesWasmPlugin
	actual    *networkservicespb.WasmPlugin
}

var _ directbase.Adapter = &WasmPluginAdapter{}

func (a *WasmPluginAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting WasmPlugin", "name", a.id)

	req := &networkservicespb.GetWasmPluginRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetWasmPlugin(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting WasmPlugin %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *WasmPluginAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating WasmPlugin", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := NetworkServicesWasmPluginSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &networkservicespb.CreateWasmPluginRequest{
		Parent:       a.id.Parent().String(),
		WasmPluginId: a.id.ID(),
		WasmPlugin:   resource,
	}
	op, err := a.gcpClient.CreateWasmPlugin(ctx, req)
	if err != nil {
		return fmt.Errorf("creating WasmPlugin %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create WasmPlugin %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created WasmPlugin", "name", a.id)

	status := &krm.NetworkServicesWasmPluginStatus{}
	status.ObservedState = NetworkServicesWasmPluginObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *WasmPluginAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating WasmPlugin", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := NetworkServicesWasmPluginSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareWasmPlugin(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &networkservicespb.UpdateWasmPluginRequest{
			WasmPlugin: desired,
			UpdateMask: updateMask,
		}
		op, err := a.gcpClient.UpdateWasmPlugin(ctx, req)
		if err != nil {
			return fmt.Errorf("updating WasmPlugin %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update WasmPlugin %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated WasmPlugin", "name", a.id)

		status := &krm.NetworkServicesWasmPluginStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = NetworkServicesWasmPluginObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareWasmPlugin(ctx context.Context, actual, desired *networkservicespb.WasmPlugin) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, NetworkServicesWasmPluginSpec_FromProto, NetworkServicesWasmPluginSpec_ToProto)
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

func (a *WasmPluginAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.NetworkServicesWasmPlugin{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(NetworkServicesWasmPluginSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.NetworkServicesWasmPluginGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *WasmPluginAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting WasmPlugin", "name", a.id)

	req := &networkservicespb.DeleteWasmPluginRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteWasmPlugin(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting WasmPlugin %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted WasmPlugin", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete WasmPlugin %s: %w", a.id, err)
		}
	}
	return true, nil
}
