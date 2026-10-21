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

package kmsautokeyconfig

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/kms/v1beta1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/kms/apiv1"
	kmspb "cloud.google.com/go/kms/apiv1/kmspb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.KMSAutokeyConfigGVK, NewAutokeyConfigModel)
}

func NewAutokeyConfigModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelAutokeyConfig{config: *config}, nil
}

var _ directbase.Model = &modelAutokeyConfig{}
var _ = strings.TrimPrefix

type modelAutokeyConfig struct {
	config config.ControllerConfig
}

func (m *modelAutokeyConfig) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.KMSAutokeyConfig{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.KMSAutokeyConfigIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &AutokeyConfigAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelAutokeyConfig) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelAutokeyConfig) client(ctx context.Context) (*gcp.AutokeyAdminClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewAutokeyAdminRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building AutokeyConfig client: %w", err)
	}
	return gcpClient, err
}

type AutokeyConfigAdapter struct {
	id        *krm.KMSAutokeyConfigIdentity
	gcpClient *gcp.AutokeyAdminClient
	desired   *krm.KMSAutokeyConfig
	actual    *kmspb.AutokeyConfig
}

var _ directbase.Adapter = &AutokeyConfigAdapter{}

func (a *AutokeyConfigAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting AutokeyConfig", "name", a.id)

	req := &kmspb.GetAutokeyConfigRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetAutokeyConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting AutokeyConfig %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *AutokeyConfigAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating AutokeyConfig", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := KMSAutokeyConfigSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &kmspb.CreateAutokeyConfigRequest{
		Parent:          a.id.ParentString(),
		AutokeyConfigId: a.id.AutokeyConfig,
		AutokeyConfig:   resource,
	}
	created, err := a.gcpClient.CreateAutokeyConfig(ctx, req)
	if err != nil {
		return fmt.Errorf("creating AutokeyConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created AutokeyConfig", "name", a.id)

	status := &krm.KMSAutokeyConfigStatus{}
	status.ObservedState = KMSAutokeyConfigObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *AutokeyConfigAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating AutokeyConfig", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := KMSAutokeyConfigSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareAutokeyConfig(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &kmspb.UpdateAutokeyConfigRequest{
			AutokeyConfig: desired,
			UpdateMask:    updateMask,
		}
		updated, err := a.gcpClient.UpdateAutokeyConfig(ctx, req)
		if err != nil {
			return fmt.Errorf("updating AutokeyConfig %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated AutokeyConfig", "name", a.id)

		status := &krm.KMSAutokeyConfigStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = KMSAutokeyConfigObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareAutokeyConfig(ctx context.Context, actual, desired *kmspb.AutokeyConfig) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, KMSAutokeyConfigSpec_FromProto, KMSAutokeyConfigSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *AutokeyConfigAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.KMSAutokeyConfig{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(KMSAutokeyConfigSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.AutokeyConfig)
	u.SetGroupVersionKind(krm.KMSAutokeyConfigGVK)
	return u, nil
}

func (a *AutokeyConfigAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deletion not supported for AutokeyConfig, removing finalizer without deleting GCP resource", "name", a.id)
	return true, nil
}
