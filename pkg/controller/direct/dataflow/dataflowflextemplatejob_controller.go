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

package dataflow

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/dataflow/v1beta1"
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

	gcp "cloud.google.com/go/dataflow/apiv1beta3"
	pb "cloud.google.com/go/dataflow/apiv1beta3/dataflowpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.DataflowFlexTemplateJobGVK, NewDataflowFlexTemplateJobModel)
}

func NewDataflowFlexTemplateJobModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelDataflowFlexTemplateJob{config: *config}, nil
}

var _ directbase.Model = &modelDataflowFlexTemplateJob{}
var _ = strings.TrimPrefix

type modelDataflowFlexTemplateJob struct {
	config config.ControllerConfig
}

func (m *modelDataflowFlexTemplateJob) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.DataflowFlexTemplateJob{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.DataflowFlexTemplateJobIdentity{}
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &DataflowFlexTemplateJobAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelDataflowFlexTemplateJob) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelDataflowFlexTemplateJob) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building DataflowFlexTemplateJob client: %w", err)
	}
	return gcpClient, err
}

type DataflowFlexTemplateJobAdapter struct {
	id        *krm.DataflowFlexTemplateJobIdentity
	gcpClient *gcp.Client
	desired   *krm.DataflowFlexTemplateJob
	actual    *pb.DataflowFlexTemplateJob
}

var _ directbase.Adapter = &DataflowFlexTemplateJobAdapter{}

func (a *DataflowFlexTemplateJobAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting DataflowFlexTemplateJob", "name", a.id)

	req := &pb.GetDataflowFlexTemplateJobRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetDataflowFlexTemplateJob(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting DataflowFlexTemplateJob %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *DataflowFlexTemplateJobAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating DataflowFlexTemplateJob", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := DataflowFlexTemplateJobSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateDataflowFlexTemplateJobRequest{
		Parent:                    a.id.ParentString(),
		DataflowFlexTemplateJobId: a.id.DataflowFlexTemplateJob,
		DataflowFlexTemplateJob:   resource,
	}
	created, err := a.gcpClient.CreateDataflowFlexTemplateJob(ctx, req)
	if err != nil {
		return fmt.Errorf("creating DataflowFlexTemplateJob %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created DataflowFlexTemplateJob", "name", a.id)

	status := &krm.DataflowFlexTemplateJobStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *DataflowFlexTemplateJobAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating DataflowFlexTemplateJob", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := DataflowFlexTemplateJobSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareDataflowFlexTemplateJob(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("DataflowFlexTemplateJob resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.DataflowFlexTemplateJobStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareDataflowFlexTemplateJob(ctx context.Context, actual, desired *pb.DataflowFlexTemplateJob) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, DataflowFlexTemplateJobSpec_FromProto, DataflowFlexTemplateJobSpec_ToProto)
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

func (a *DataflowFlexTemplateJobAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.DataflowFlexTemplateJob{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(DataflowFlexTemplateJobSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ResourceID = direct.LazyPtr(a.id.DataflowFlexTemplateJob)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.DataflowFlexTemplateJob)
	u.SetGroupVersionKind(krm.DataflowFlexTemplateJobGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *DataflowFlexTemplateJobAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deletion not supported for DataflowFlexTemplateJob, removing finalizer without deleting GCP resource", "name", a.id)
	return true, nil
}
