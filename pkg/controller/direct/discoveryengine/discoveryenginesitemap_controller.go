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
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/discoveryengine/apiv1"
	pb "cloud.google.com/go/discoveryengine/apiv1/discoveryenginepb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.DiscoveryEngineSitemapGVK, NewDiscoveryEngineSitemapModel)
}

func NewDiscoveryEngineSitemapModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelDiscoveryEngineSitemap{config: *config}, nil
}

var _ directbase.Model = &modelDiscoveryEngineSitemap{}
var _ = strings.TrimPrefix

type modelDiscoveryEngineSitemap struct {
	config config.ControllerConfig
}

func (m *modelDiscoveryEngineSitemap) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.DiscoveryEngineSitemap{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.DiscoveryEngineSitemapIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &DiscoveryEngineSitemapAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelDiscoveryEngineSitemap) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.DiscoveryEngineSitemapIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &DiscoveryEngineSitemapAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelDiscoveryEngineSitemap) client(ctx context.Context) (*gcp.SiteSearchEngineClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewSiteSearchEngineRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building DiscoveryEngineSitemap client: %w", err)
	}
	return gcpClient, err
}

type DiscoveryEngineSitemapAdapter struct {
	id        *krm.DiscoveryEngineSitemapIdentity
	gcpClient *gcp.SiteSearchEngineClient
	desired   *krm.DiscoveryEngineSitemap
	actual    *pb.DiscoveryEngineSitemap
}

var _ directbase.Adapter = &DiscoveryEngineSitemapAdapter{}

func (a *DiscoveryEngineSitemapAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting DiscoveryEngineSitemap", "name", a.id)

	req := &pb.GetDiscoveryEngineSitemapRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetDiscoveryEngineSitemap(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting DiscoveryEngineSitemap %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *DiscoveryEngineSitemapAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating DiscoveryEngineSitemap", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := DiscoveryEngineSitemapSpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateDiscoveryEngineSitemapRequest{
		Parent:                   a.id.ParentString(),
		DiscoveryEngineSitemapId: a.id.Sitemap,
		DiscoveryEngineSitemap:   resource,
	}
	op, err := a.gcpClient.CreateDiscoveryEngineSitemap(ctx, req)
	if err != nil {
		return fmt.Errorf("creating DiscoveryEngineSitemap %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create DiscoveryEngineSitemap %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created DiscoveryEngineSitemap", "name", a.id)

	status := &krm.DiscoveryEngineSitemapStatus{}
	status.ObservedState = DiscoveryEngineSitemapObservedState_v1alpha1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *DiscoveryEngineSitemapAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating DiscoveryEngineSitemap", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := DiscoveryEngineSitemapSpec_v1alpha1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareDiscoveryEngineSitemap(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("DiscoveryEngineSitemap resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.DiscoveryEngineSitemapStatus{}
	mapCtx = &direct.MapContext{}
	status.ObservedState = DiscoveryEngineSitemapObservedState_v1alpha1_FromProto(mapCtx, a.actual)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareDiscoveryEngineSitemap(ctx context.Context, actual, desired *pb.DiscoveryEngineSitemap) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, DiscoveryEngineSitemapSpec_v1alpha1_FromProto, DiscoveryEngineSitemapSpec_v1alpha1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *DiscoveryEngineSitemapAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.DiscoveryEngineSitemap{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(DiscoveryEngineSitemapSpec_v1alpha1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Sitemap)
	u.SetGroupVersionKind(krm.DiscoveryEngineSitemapGVK)
	return u, nil
}

func (a *DiscoveryEngineSitemapAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deletion not supported for DiscoveryEngineSitemap, removing finalizer without deleting GCP resource", "name", a.id)
	return true, nil
}
