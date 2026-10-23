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

package storageinsights

import (
	"context"
	"fmt"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/storageinsights/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/export"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/label"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/storageinsights/apiv1"
	pb "cloud.google.com/go/storageinsights/apiv1/storageinsightspb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.StorageInsightsDatasetConfigGVK, NewDatasetConfigModel)
}

func NewDatasetConfigModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelDatasetConfig{config: *config}, nil
}

var _ directbase.Model = &modelDatasetConfig{}
var _ = strings.TrimPrefix

type modelDatasetConfig struct {
	config config.ControllerConfig
}

func (m *modelDatasetConfig) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.StorageInsightsDatasetConfig{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.StorageInsightsDatasetConfigIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &DatasetConfigAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelDatasetConfig) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.StorageInsightsDatasetConfigIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &DatasetConfigAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelDatasetConfig) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building DatasetConfig client: %w", err)
	}
	return gcpClient, err
}

type DatasetConfigAdapter struct {
	id        *krm.StorageInsightsDatasetConfigIdentity
	gcpClient *gcp.Client
	desired   *krm.StorageInsightsDatasetConfig
	actual    *pb.DatasetConfig
}

var _ directbase.Adapter = &DatasetConfigAdapter{}

func (a *DatasetConfigAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting DatasetConfig", "name", a.id)

	req := &pb.GetDatasetConfigRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetDatasetConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting DatasetConfig %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *DatasetConfigAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating DatasetConfig", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := StorageInsightsDatasetConfigSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateDatasetConfigRequest{
		Parent:          a.id.ParentString(),
		DatasetConfigId: a.id.DatasetConfig,
		DatasetConfig:   resource,
	}
	op, err := a.gcpClient.CreateDatasetConfig(ctx, req)
	if err != nil {
		return fmt.Errorf("creating DatasetConfig %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create DatasetConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created DatasetConfig", "name", a.id)

	status := &krm.StorageInsightsDatasetConfigStatus{}
	status.ObservedState = StorageInsightsDatasetConfigObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *DatasetConfigAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating DatasetConfig", "name", a.id)

	desired, diffs, updateMask, err := compareDatasetConfig(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateDatasetConfigRequest{
			DatasetConfig: desired,
			UpdateMask:    updateMask,
		}
		op, err := a.gcpClient.UpdateDatasetConfig(ctx, req)
		if err != nil {
			return fmt.Errorf("updating DatasetConfig %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update DatasetConfig %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated DatasetConfig", "name", a.id)

		status := &krm.StorageInsightsDatasetConfigStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = StorageInsightsDatasetConfigObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareDatasetConfig(ctx context.Context, u *unstructured.Unstructured, actual *pb.DatasetConfig, desiredSpec *krm.StorageInsightsDatasetConfigSpec) (*pb.DatasetConfig, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	return common.CompareGreenfieldSpecAndLabels(
		ctx,
		u,
		desiredSpec,
		actual,
		"labels",
		StorageInsightsDatasetConfigSpec_FromProto,
		StorageInsightsDatasetConfigSpec_ToProto,
		nil,
	)
}

func (a *DatasetConfigAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.StorageInsightsDatasetConfig{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(StorageInsightsDatasetConfigSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = &a.id.Location
	obj.Spec.ResourceID = direct.PtrTo(a.id.DatasetConfig)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.DatasetConfig)
	u.SetGroupVersionKind(krm.StorageInsightsDatasetConfigGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *DatasetConfigAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting DatasetConfig", "name", a.id)

	req := &pb.DeleteDatasetConfigRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteDatasetConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting DatasetConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted DatasetConfig", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete DatasetConfig %s: %w", a.id, err)
		}
	}
	return true, nil
}
