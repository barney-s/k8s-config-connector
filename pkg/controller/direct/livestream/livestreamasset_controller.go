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

package livestream

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/livestream/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/video/livestream/apiv1"
	pb "cloud.google.com/go/video/livestream/apiv1/livestreampb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.LiveStreamAssetGVK, NewAssetModel)
}

func NewAssetModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelAsset{config: *config}, nil
}

var _ directbase.Model = &modelAsset{}
var _ = strings.TrimPrefix

type modelAsset struct {
	config config.ControllerConfig
}

func (m *modelAsset) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.LiveStreamAsset{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.LiveStreamAssetIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &AssetAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelAsset) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelAsset) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Asset client: %w", err)
	}
	return gcpClient, err
}

type AssetAdapter struct {
	id        *krm.LiveStreamAssetIdentity
	gcpClient *gcp.Client
	desired   *krm.LiveStreamAsset
	actual    *pb.Asset
}

var _ directbase.Adapter = &AssetAdapter{}

func (a *AssetAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Asset", "name", a.id)

	req := &pb.GetAssetRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetAsset(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Asset %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *AssetAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Asset", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := LiveStreamAssetSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateAssetRequest{
		Parent:  a.id.ParentString(),
		AssetId: a.id.Asset,
		Asset:   resource,
	}
	op, err := a.gcpClient.CreateAsset(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Asset %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create Asset %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Asset", "name", a.id)

	status := &krm.LiveStreamAssetStatus{}
	status.ObservedState = LiveStreamAssetObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *AssetAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Asset", "name", a.id)

	desired, diffs, updateMask, err := compareAsset(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	_ = desired
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("LiveStreamAsset resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.LiveStreamAssetStatus{}
	mapCtx := &direct.MapContext{}
	status.ObservedState = LiveStreamAssetObservedState_FromProto(mapCtx, a.actual)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareAsset(ctx context.Context, u *unstructured.Unstructured, actual *pb.Asset, desiredSpec *krm.LiveStreamAssetSpec) (*pb.Asset, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		LiveStreamAssetSpec_FromProto,
		LiveStreamAssetSpec_ToProto,
		nil,
	)
}

func (a *AssetAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.LiveStreamAsset{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(LiveStreamAssetSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = direct.LazyPtr(a.id.Location)
	obj.Spec.ResourceID = direct.LazyPtr(a.id.Asset)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Asset)
	u.SetGroupVersionKind(krm.LiveStreamAssetGVK)
	return u, nil
}

func (a *AssetAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Asset", "name", a.id)

	req := &pb.DeleteAssetRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteAsset(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Asset %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Asset", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete Asset %s: %w", a.id, err)
		}
	}
	return true, nil
}
