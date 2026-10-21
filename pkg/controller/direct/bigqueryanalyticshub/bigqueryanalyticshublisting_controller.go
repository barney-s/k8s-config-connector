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

package bigqueryanalyticshub

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/bigqueryanalyticshub/v1beta1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/bigquery/analyticshub/apiv1"
	bigqueryanalyticshubpb "cloud.google.com/go/bigquery/analyticshub/apiv1/analyticshubpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.BigQueryAnalyticsHubListingGVK, NewListingModel)
}

func NewListingModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelListing{config: *config}, nil
}

var _ directbase.Model = &modelListing{}
var _ = strings.TrimPrefix

type modelListing struct {
	config config.ControllerConfig
}

func (m *modelListing) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.BigQueryAnalyticsHubListing{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.BigQueryAnalyticsHubListingIdentity)

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ListingAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelListing) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelListing) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Listing client: %w", err)
	}
	return gcpClient, err
}

type ListingAdapter struct {
	id        *krm.BigQueryAnalyticsHubListingIdentity
	gcpClient *gcp.Client
	desired   *krm.BigQueryAnalyticsHubListing
	actual    *bigqueryanalyticshubpb.Listing
}

var _ directbase.Adapter = &ListingAdapter{}

func (a *ListingAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Listing", "name", a.id)

	req := &bigqueryanalyticshubpb.GetListingRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetListing(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Listing %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ListingAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Listing", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := BigQueryAnalyticsHubListingSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &bigqueryanalyticshubpb.CreateListingRequest{
		Parent:    a.id.ParentString(),
		ListingId: a.id.DataExchange,
		Listing:   resource,
	}
	created, err := a.gcpClient.CreateListing(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Listing %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Listing", "name", a.id)

	status := &krm.BigQueryAnalyticsHubListingStatus{}
	status.ObservedState = BigQueryAnalyticsHubListingObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ListingAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Listing", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := BigQueryAnalyticsHubListingSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareListing(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &bigqueryanalyticshubpb.UpdateListingRequest{
			Listing:    desired,
			UpdateMask: updateMask,
		}
		updated, err := a.gcpClient.UpdateListing(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Listing %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Listing", "name", a.id)

		status := &krm.BigQueryAnalyticsHubListingStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = BigQueryAnalyticsHubListingObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareListing(ctx context.Context, actual, desired *bigqueryanalyticshubpb.Listing) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, BigQueryAnalyticsHubListingSpec_FromProto, BigQueryAnalyticsHubListingSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *ListingAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.BigQueryAnalyticsHubListing{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(BigQueryAnalyticsHubListingSpec_FromProto(mapCtx, a.actual))
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
	u.SetName(a.id.DataExchange)
	u.SetGroupVersionKind(krm.BigQueryAnalyticsHubListingGVK)
	return u, nil
}

func (a *ListingAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Listing", "name", a.id)

	req := &bigqueryanalyticshubpb.DeleteListingRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteListing(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Listing %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Listing", "name", a.id)
	return true, nil
}
