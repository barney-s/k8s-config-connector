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
	registry.RegisterModel(krm.NetworkServicesLBEdgeExtensionGVK, NewLbEdgeExtensionModel)
}

func NewLbEdgeExtensionModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelLbEdgeExtension{config: *config}, nil
}

var _ directbase.Model = &modelLbEdgeExtension{}
var _ = strings.TrimPrefix

type modelLbEdgeExtension struct {
	config config.ControllerConfig
}

func (m *modelLbEdgeExtension) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.NetworkServicesLBEdgeExtension{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.LBEdgeExtensionIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &LbEdgeExtensionAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelLbEdgeExtension) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelLbEdgeExtension) client(ctx context.Context) (*gcp.DepClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewDepRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building LbEdgeExtension client: %w", err)
	}
	return gcpClient, err
}

type LbEdgeExtensionAdapter struct {
	id        *krm.LBEdgeExtensionIdentity
	gcpClient *gcp.DepClient
	desired   *krm.NetworkServicesLBEdgeExtension
	actual    *networkservicespb.LbEdgeExtension
}

var _ directbase.Adapter = &LbEdgeExtensionAdapter{}

func (a *LbEdgeExtensionAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting LbEdgeExtension", "name", a.id)

	req := &networkservicespb.GetLbEdgeExtensionRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetLbEdgeExtension(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting LbEdgeExtension %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *LbEdgeExtensionAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating LbEdgeExtension", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := NetworkServicesLBEdgeExtensionSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &networkservicespb.CreateLbEdgeExtensionRequest{
		Parent:            a.id.ParentString(),
		LbEdgeExtensionId: a.id.LbEdgeExtension,
		LbEdgeExtension:   resource,
	}
	op, err := a.gcpClient.CreateLbEdgeExtension(ctx, req)
	if err != nil {
		return fmt.Errorf("creating LbEdgeExtension %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create LbEdgeExtension %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created LbEdgeExtension", "name", a.id)

	status := &krm.NetworkServicesLBEdgeExtensionStatus{}
	status.ObservedState = NetworkServicesLBEdgeExtensionObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *LbEdgeExtensionAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating LbEdgeExtension", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := NetworkServicesLBEdgeExtensionSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareLbEdgeExtension(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &networkservicespb.UpdateLbEdgeExtensionRequest{
			LbEdgeExtension: desired,
			UpdateMask:      updateMask,
		}
		op, err := a.gcpClient.UpdateLbEdgeExtension(ctx, req)
		if err != nil {
			return fmt.Errorf("updating LbEdgeExtension %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update LbEdgeExtension %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated LbEdgeExtension", "name", a.id)

		status := &krm.NetworkServicesLBEdgeExtensionStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = NetworkServicesLBEdgeExtensionObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareLbEdgeExtension(ctx context.Context, actual, desired *networkservicespb.LbEdgeExtension) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, NetworkServicesLBEdgeExtensionSpec_FromProto, NetworkServicesLBEdgeExtensionSpec_ToProto)
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

func (a *LbEdgeExtensionAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.NetworkServicesLBEdgeExtension{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(NetworkServicesLBEdgeExtensionSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.LbEdgeExtension)
	u.SetGroupVersionKind(krm.NetworkServicesLBEdgeExtensionGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *LbEdgeExtensionAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting LbEdgeExtension", "name", a.id)

	req := &networkservicespb.DeleteLbEdgeExtensionRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteLbEdgeExtension(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting LbEdgeExtension %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted LbEdgeExtension", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete LbEdgeExtension %s: %w", a.id, err)
		}
	}
	return true, nil
}
