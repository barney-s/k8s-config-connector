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

package dataplex

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/dataplex/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/export"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/label"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/dataplex/apiv1"
	pb "cloud.google.com/go/dataplex/apiv1/dataplexpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.DataplexMetadataJobGVK, NewMetadataJobModel)
}

func NewMetadataJobModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelMetadataJob{config: *config}, nil
}

var _ directbase.Model = &modelMetadataJob{}
var _ = strings.TrimPrefix

type modelMetadataJob struct {
	config config.ControllerConfig
}

func (m *modelMetadataJob) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.DataplexMetadataJob{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.MetadataJobIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &MetadataJobAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelMetadataJob) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelMetadataJob) client(ctx context.Context) (*gcp.CatalogClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewCatalogRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building MetadataJob client: %w", err)
	}
	return gcpClient, err
}

type MetadataJobAdapter struct {
	id        *krm.MetadataJobIdentity
	gcpClient *gcp.CatalogClient
	desired   *krm.DataplexMetadataJob
	actual    *pb.MetadataJob
}

var _ directbase.Adapter = &MetadataJobAdapter{}

func (a *MetadataJobAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting MetadataJob", "name", a.id)

	req := &pb.GetMetadataJobRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetMetadataJob(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting MetadataJob %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *MetadataJobAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating MetadataJob", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := DataplexMetadataJobSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateMetadataJobRequest{
		Parent:        a.id.ParentString(),
		MetadataJobId: a.id.MetadataJob,
		MetadataJob:   resource,
	}
	op, err := a.gcpClient.CreateMetadataJob(ctx, req)
	if err != nil {
		return fmt.Errorf("creating MetadataJob %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create MetadataJob %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created MetadataJob", "name", a.id)

	status := &krm.DataplexMetadataJobStatus{}
	status.ObservedState = DataplexMetadataJobObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *MetadataJobAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating MetadataJob", "name", a.id)

	desired, diffs, updateMask, err := compareMetadataJob(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	_ = desired
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("DataplexMetadataJob resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.DataplexMetadataJobStatus{}
	mapCtx := &direct.MapContext{}
	status.ObservedState = DataplexMetadataJobObservedState_FromProto(mapCtx, a.actual)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareMetadataJob(ctx context.Context, u *unstructured.Unstructured, actual *pb.MetadataJob, desiredSpec *krm.DataplexMetadataJobSpec) (*pb.MetadataJob, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	return common.CompareGreenfieldSpecAndLabels(
		ctx,
		u,
		desiredSpec,
		actual,
		"labels",
		DataplexMetadataJobSpec_FromProto,
		DataplexMetadataJobSpec_ToProto,
		nil,
	)
}

func (a *MetadataJobAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.DataplexMetadataJob{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(DataplexMetadataJobSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.MetadataJob)
	u.SetGroupVersionKind(krm.DataplexMetadataJobGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *MetadataJobAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deletion not supported for MetadataJob, removing finalizer without deleting GCP resource", "name", a.id)
	return true, nil
}
