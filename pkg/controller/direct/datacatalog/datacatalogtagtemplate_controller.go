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

package datacatalog

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/datacatalog/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/datacatalog/apiv1"
	pb "cloud.google.com/go/datacatalog/apiv1/datacatalogpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.DataCatalogTagTemplateGVK, NewTagTemplateModel)
}

func NewTagTemplateModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelTagTemplate{config: *config}, nil
}

var _ directbase.Model = &modelTagTemplate{}
var _ = strings.TrimPrefix

type modelTagTemplate struct {
	config config.ControllerConfig
}

func (m *modelTagTemplate) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.DataCatalogTagTemplate{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.TagTemplateIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &TagTemplateAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelTagTemplate) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelTagTemplate) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building TagTemplate client: %w", err)
	}
	return gcpClient, err
}

type TagTemplateAdapter struct {
	id        *krm.TagTemplateIdentity
	gcpClient *gcp.Client
	desired   *krm.DataCatalogTagTemplate
	actual    *pb.TagTemplate
}

var _ directbase.Adapter = &TagTemplateAdapter{}

func (a *TagTemplateAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting TagTemplate", "name", a.id)

	req := &pb.GetTagTemplateRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetTagTemplate(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting TagTemplate %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *TagTemplateAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating TagTemplate", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := DataCatalogTagTemplateSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateTagTemplateRequest{
		Parent:        a.id.Parent().String(),
		TagTemplateId: a.id.ID(),
		TagTemplate:   resource,
	}
	created, err := a.gcpClient.CreateTagTemplate(ctx, req)
	if err != nil {
		return fmt.Errorf("creating TagTemplate %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created TagTemplate", "name", a.id)

	status := &krm.DataCatalogTagTemplateStatus{}
	status.ObservedState = DataCatalogTagTemplateObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *TagTemplateAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating TagTemplate", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := DataCatalogTagTemplateSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareTagTemplate(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateTagTemplateRequest{
			TagTemplate: desired,
			UpdateMask:  updateMask,
		}
		updated, err := a.gcpClient.UpdateTagTemplate(ctx, req)
		if err != nil {
			return fmt.Errorf("updating TagTemplate %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated TagTemplate", "name", a.id)

		status := &krm.DataCatalogTagTemplateStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = DataCatalogTagTemplateObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareTagTemplate(ctx context.Context, actual, desired *pb.TagTemplate) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, DataCatalogTagTemplateSpec_FromProto, DataCatalogTagTemplateSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *TagTemplateAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.DataCatalogTagTemplate{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(DataCatalogTagTemplateSpec_FromProto(mapCtx, a.actual))
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
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.DataCatalogTagTemplateGVK)
	return u, nil
}

func (a *TagTemplateAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting TagTemplate", "name", a.id)

	req := &pb.DeleteTagTemplateRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteTagTemplate(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting TagTemplate %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted TagTemplate", "name", a.id)
	return true, nil
}
