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
	registry.RegisterModel(krm.DataplexDataTaxonomyGVK, NewDataTaxonomyModel)
}

func NewDataTaxonomyModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelDataTaxonomy{config: *config}, nil
}

var _ directbase.Model = &modelDataTaxonomy{}
var _ = strings.TrimPrefix

type modelDataTaxonomy struct {
	config config.ControllerConfig
}

func (m *modelDataTaxonomy) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.DataplexDataTaxonomy{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.DataTaxonomyIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &DataTaxonomyAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelDataTaxonomy) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelDataTaxonomy) client(ctx context.Context) (*gcp.DataTaxonomyClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewDataTaxonomyRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building DataTaxonomy client: %w", err)
	}
	return gcpClient, err
}

type DataTaxonomyAdapter struct {
	id        *krm.DataTaxonomyIdentity
	gcpClient *gcp.DataTaxonomyClient
	desired   *krm.DataplexDataTaxonomy
	actual    *pb.DataTaxonomy
}

var _ directbase.Adapter = &DataTaxonomyAdapter{}

func (a *DataTaxonomyAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting DataTaxonomy", "name", a.id)

	req := &pb.GetDataTaxonomyRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetDataTaxonomy(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting DataTaxonomy %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *DataTaxonomyAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating DataTaxonomy", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := DataplexDataTaxonomySpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateDataTaxonomyRequest{
		Parent:         a.id.ParentString(),
		DataTaxonomyId: a.id.DataTaxonomy,
		DataTaxonomy:   resource,
	}
	op, err := a.gcpClient.CreateDataTaxonomy(ctx, req)
	if err != nil {
		return fmt.Errorf("creating DataTaxonomy %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create DataTaxonomy %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created DataTaxonomy", "name", a.id)

	status := &krm.DataplexDataTaxonomyStatus{}
	status.ObservedState = DataplexDataTaxonomyObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *DataTaxonomyAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating DataTaxonomy", "name", a.id)

	desired, diffs, updateMask, err := compareDataTaxonomy(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateDataTaxonomyRequest{
			DataTaxonomy: desired,
			UpdateMask:   updateMask,
		}
		op, err := a.gcpClient.UpdateDataTaxonomy(ctx, req)
		if err != nil {
			return fmt.Errorf("updating DataTaxonomy %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update DataTaxonomy %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated DataTaxonomy", "name", a.id)

		status := &krm.DataplexDataTaxonomyStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = DataplexDataTaxonomyObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareDataTaxonomy(ctx context.Context, u *unstructured.Unstructured, actual *pb.DataTaxonomy, desiredSpec *krm.DataplexDataTaxonomySpec) (*pb.DataTaxonomy, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	return common.CompareGreenfieldSpecAndLabels(
		ctx,
		u,
		desiredSpec,
		actual,
		"labels",
		DataplexDataTaxonomySpec_FromProto,
		DataplexDataTaxonomySpec_ToProto,
		nil,
	)
}

func (a *DataTaxonomyAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.DataplexDataTaxonomy{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(DataplexDataTaxonomySpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.DataTaxonomy)
	u.SetGroupVersionKind(krm.DataplexDataTaxonomyGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *DataTaxonomyAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting DataTaxonomy", "name", a.id)

	req := &pb.DeleteDataTaxonomyRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteDataTaxonomy(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting DataTaxonomy %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted DataTaxonomy", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete DataTaxonomy %s: %w", a.id, err)
		}
	}
	return true, nil
}
