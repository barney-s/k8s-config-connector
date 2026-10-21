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

package apihubcuration

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/apihub/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/apihub/apiv1"
	pb "cloud.google.com/go/apihub/apiv1/apihubpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.APIHubCurationGVK, NewCurationModel)
}

func NewCurationModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelCuration{config: *config}, nil
}

var _ directbase.Model = &modelCuration{}
var _ = strings.TrimPrefix

type modelCuration struct {
	config config.ControllerConfig
}

func (m *modelCuration) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.APIHubCuration{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.APIHubCurationIdentity)

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &CurationAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelCuration) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelCuration) client(ctx context.Context) (*gcp.ApiHubCurateClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewApiHubCurateRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Curation client: %w", err)
	}
	return gcpClient, err
}

type CurationAdapter struct {
	id        *krm.APIHubCurationIdentity
	gcpClient *gcp.ApiHubCurateClient
	desired   *krm.APIHubCuration
	actual    *pb.Curation
}

var _ directbase.Adapter = &CurationAdapter{}

func (a *CurationAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Curation", "name", a.id)

	req := &pb.GetCurationRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetCuration(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Curation %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *CurationAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Curation", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := APIHubCurationSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateCurationRequest{
		Parent:     a.id.ParentString(),
		CurationId: a.id.Curation,
		Curation:   resource,
	}
	created, err := a.gcpClient.CreateCuration(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Curation %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Curation", "name", a.id)

	status := &krm.APIHubCurationStatus{}
	status.ObservedState = APIHubCurationObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *CurationAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Curation", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := APIHubCurationSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareCuration(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateCurationRequest{
			Curation:   desired,
			UpdateMask: updateMask,
		}
		updated, err := a.gcpClient.UpdateCuration(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Curation %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Curation", "name", a.id)

		status := &krm.APIHubCurationStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = APIHubCurationObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareCuration(ctx context.Context, actual, desired *pb.Curation) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, APIHubCurationSpec_FromProto, APIHubCurationSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *CurationAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.APIHubCuration{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(APIHubCurationSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = a.id.Location
	obj.Spec.ResourceID = direct.LazyPtr(a.id.Curation)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Curation)
	u.SetGroupVersionKind(krm.APIHubCurationGVK)
	return u, nil
}

func (a *CurationAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Curation", "name", a.id)

	req := &pb.DeleteCurationRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteCuration(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Curation %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Curation", "name", a.id)
	return true, nil
}
