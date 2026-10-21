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

package discoveryengine

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/discoveryengine/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/discoveryengine/apiv1beta"
	discoveryenginepb "cloud.google.com/go/discoveryengine/apiv1beta/discoveryenginepb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.DiscoveryEngineServingConfigGVK, NewServingConfigModel)
}

func NewServingConfigModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelServingConfig{config: *config}, nil
}

var _ directbase.Model = &modelServingConfig{}
var _ = strings.TrimPrefix

type modelServingConfig struct {
	config config.ControllerConfig
}

func (m *modelServingConfig) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.DiscoveryEngineServingConfig{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.DiscoveryEngineServingConfigIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ServingConfigAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelServingConfig) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.DiscoveryEngineServingConfigIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ServingConfigAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelServingConfig) client(ctx context.Context) (*gcp.ServingConfigClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewServingConfigRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ServingConfig client: %w", err)
	}
	return gcpClient, err
}

type ServingConfigAdapter struct {
	id        *krm.DiscoveryEngineServingConfigIdentity
	gcpClient *gcp.ServingConfigClient
	desired   *krm.DiscoveryEngineServingConfig
	actual    *discoveryenginepb.ServingConfig
}

var _ directbase.Adapter = &ServingConfigAdapter{}

func (a *ServingConfigAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ServingConfig", "name", a.id)

	req := &discoveryenginepb.GetServingConfigRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetServingConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ServingConfig %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ServingConfigAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ServingConfig", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := DiscoveryEngineServingConfigSpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &discoveryenginepb.CreateServingConfigRequest{
		Parent:          a.id.ParentString(),
		ServingConfigId: a.id.ServingConfig,
		ServingConfig:   resource,
	}
	created, err := a.gcpClient.CreateServingConfig(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ServingConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ServingConfig", "name", a.id)

	status := &krm.DiscoveryEngineServingConfigStatus{}
	status.ObservedState = DiscoveryEngineServingConfigObservedState_v1alpha1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ServingConfigAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ServingConfig", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := DiscoveryEngineServingConfigSpec_v1alpha1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareServingConfig(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &discoveryenginepb.UpdateServingConfigRequest{
			ServingConfig: desired,
			UpdateMask:    updateMask,
		}
		updated, err := a.gcpClient.UpdateServingConfig(ctx, req)
		if err != nil {
			return fmt.Errorf("updating ServingConfig %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated ServingConfig", "name", a.id)

		status := &krm.DiscoveryEngineServingConfigStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = DiscoveryEngineServingConfigObservedState_v1alpha1_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareServingConfig(ctx context.Context, actual, desired *discoveryenginepb.ServingConfig) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, DiscoveryEngineServingConfigSpec_v1alpha1_FromProto, DiscoveryEngineServingConfigSpec_v1alpha1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *ServingConfigAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.DiscoveryEngineServingConfig{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(DiscoveryEngineServingConfigSpec_v1alpha1_FromProto(mapCtx, a.actual))
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
	u.SetName(a.id.ServingConfig)
	u.SetGroupVersionKind(krm.DiscoveryEngineServingConfigGVK)
	return u, nil
}

func (a *ServingConfigAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting ServingConfig", "name", a.id)

	req := &discoveryenginepb.DeleteServingConfigRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteServingConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting ServingConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted ServingConfig", "name", a.id)
	return true, nil
}
