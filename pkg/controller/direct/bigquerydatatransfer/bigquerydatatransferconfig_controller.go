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

package bigquerydatatransfer

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/bigquerydatatransfer/v1beta1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/bigquery/datatransfer/apiv1"
	bigquerydatatransferpb "cloud.google.com/go/bigquery/datatransfer/apiv1/datatransferpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.BigQueryDataTransferConfigGVK, NewTransferConfigModel)
}

func NewTransferConfigModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelTransferConfig{config: *config}, nil
}

var _ directbase.Model = &modelTransferConfig{}
var _ = strings.TrimPrefix

type modelTransferConfig struct {
	config config.ControllerConfig
}

func (m *modelTransferConfig) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.BigQueryDataTransferConfig{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &BigQueryDataTransferConfigIdentity{}
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &TransferConfigAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelTransferConfig) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &BigQueryDataTransferConfigIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &TransferConfigAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelTransferConfig) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building TransferConfig client: %w", err)
	}
	return gcpClient, err
}

type TransferConfigAdapter struct {
	id        *BigQueryDataTransferConfigIdentity
	gcpClient *gcp.Client
	desired   *krm.BigQueryDataTransferConfig
	actual    *bigquerydatatransferpb.TransferConfig
}

var _ directbase.Adapter = &TransferConfigAdapter{}

func (a *TransferConfigAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting TransferConfig", "name", a.id)

	req := &bigquerydatatransferpb.GetTransferConfigRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetTransferConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting TransferConfig %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *TransferConfigAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating TransferConfig", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := BigQueryDataTransferConfigSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &bigquerydatatransferpb.CreateTransferConfigRequest{
		Parent:           a.id.ParentString(),
		TransferConfigId: a.id.transferConfigID,
		TransferConfig:   resource,
	}
	created, err := a.gcpClient.CreateTransferConfig(ctx, req)
	if err != nil {
		return fmt.Errorf("creating TransferConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created TransferConfig", "name", a.id)

	status := &krm.BigQueryDataTransferConfigStatus{}
	status.ObservedState = BigQueryDataTransferConfigObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *TransferConfigAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating TransferConfig", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := BigQueryDataTransferConfigSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareTransferConfig(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &bigquerydatatransferpb.UpdateTransferConfigRequest{
			TransferConfig: desired,
			UpdateMask:     updateMask,
		}
		updated, err := a.gcpClient.UpdateTransferConfig(ctx, req)
		if err != nil {
			return fmt.Errorf("updating TransferConfig %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated TransferConfig", "name", a.id)

		status := &krm.BigQueryDataTransferConfigStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = BigQueryDataTransferConfigObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareTransferConfig(ctx context.Context, actual, desired *bigquerydatatransferpb.TransferConfig) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, BigQueryDataTransferConfigSpec_FromProto, BigQueryDataTransferConfigSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *TransferConfigAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.BigQueryDataTransferConfig{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(BigQueryDataTransferConfigSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = a.id.Location
	obj.Spec.ResourceID = direct.LazyPtr(a.id.transferConfigID)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.transferConfigID)
	u.SetGroupVersionKind(krm.BigQueryDataTransferConfigGVK)
	return u, nil
}

func (a *TransferConfigAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting TransferConfig", "name", a.id)

	req := &bigquerydatatransferpb.DeleteTransferConfigRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteTransferConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting TransferConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted TransferConfig", "name", a.id)
	return true, nil
}
