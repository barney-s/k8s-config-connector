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
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
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
	registry.RegisterModel(krm.DataplexMetadataFeedGVK, NewMetadataFeedModel)
}

func NewMetadataFeedModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelMetadataFeed{config: *config}, nil
}

var _ directbase.Model = &modelMetadataFeed{}
var _ = strings.TrimPrefix

type modelMetadataFeed struct {
	config config.ControllerConfig
}

func (m *modelMetadataFeed) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.DataplexMetadataFeed{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.MetadataFeedIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &MetadataFeedAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelMetadataFeed) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelMetadataFeed) client(ctx context.Context) (*gcp.CatalogClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewCatalogRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building MetadataFeed client: %w", err)
	}
	return gcpClient, err
}

type MetadataFeedAdapter struct {
	id        *krm.MetadataFeedIdentity
	gcpClient *gcp.CatalogClient
	desired   *krm.DataplexMetadataFeed
	actual    *pb.MetadataFeed
}

var _ directbase.Adapter = &MetadataFeedAdapter{}

func (a *MetadataFeedAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting MetadataFeed", "name", a.id)

	req := &pb.GetMetadataFeedRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetMetadataFeed(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting MetadataFeed %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *MetadataFeedAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating MetadataFeed", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := DataplexMetadataFeedSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateMetadataFeedRequest{
		Parent:         a.id.ParentString(),
		MetadataFeedId: a.id.MetadataFeed,
		MetadataFeed:   resource,
	}
	op, err := a.gcpClient.CreateMetadataFeed(ctx, req)
	if err != nil {
		return fmt.Errorf("creating MetadataFeed %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create MetadataFeed %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created MetadataFeed", "name", a.id)

	status := &krm.DataplexMetadataFeedStatus{}
	status.ObservedState = DataplexMetadataFeedObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *MetadataFeedAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating MetadataFeed", "name", a.id)

	desired, diffs, updateMask, err := compareMetadataFeed(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateMetadataFeedRequest{
			MetadataFeed: desired,
			UpdateMask:   updateMask,
		}
		op, err := a.gcpClient.UpdateMetadataFeed(ctx, req)
		if err != nil {
			return fmt.Errorf("updating MetadataFeed %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update MetadataFeed %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated MetadataFeed", "name", a.id)

		status := &krm.DataplexMetadataFeedStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = DataplexMetadataFeedObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareMetadataFeed(ctx context.Context, u *unstructured.Unstructured, actual *pb.MetadataFeed, desiredSpec *krm.DataplexMetadataFeedSpec) (*pb.MetadataFeed, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	return common.CompareGreenfieldSpecAndLabels(
		ctx,
		u,
		desiredSpec,
		actual,
		"labels",
		DataplexMetadataFeedSpec_FromProto,
		DataplexMetadataFeedSpec_ToProto,
		nil,
	)
}

func (a *MetadataFeedAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.DataplexMetadataFeed{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(DataplexMetadataFeedSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = &a.id.Location

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.MetadataFeed)
	u.SetGroupVersionKind(krm.DataplexMetadataFeedGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *MetadataFeedAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting MetadataFeed", "name", a.id)

	req := &pb.DeleteMetadataFeedRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteMetadataFeed(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting MetadataFeed %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted MetadataFeed", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete MetadataFeed %s: %w", a.id, err)
		}
	}
	return true, nil
}
