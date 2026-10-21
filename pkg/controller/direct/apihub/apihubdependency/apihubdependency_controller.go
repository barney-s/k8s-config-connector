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

package apihubdependency

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
	registry.RegisterModel(krm.APIHubDependencyGVK, NewDependencyModel)
}

func NewDependencyModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelDependency{config: *config}, nil
}

var _ directbase.Model = &modelDependency{}
var _ = strings.TrimPrefix

type modelDependency struct {
	config config.ControllerConfig
}

func (m *modelDependency) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.APIHubDependency{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.APIHubDependencyIdentity)

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &DependencyAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelDependency) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelDependency) client(ctx context.Context) (*gcp.ApiHubDependenciesClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewApiHubDependenciesRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Dependency client: %w", err)
	}
	return gcpClient, err
}

type DependencyAdapter struct {
	id        *krm.APIHubDependencyIdentity
	gcpClient *gcp.ApiHubDependenciesClient
	desired   *krm.APIHubDependency
	actual    *pb.Dependency
}

var _ directbase.Adapter = &DependencyAdapter{}

func (a *DependencyAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Dependency", "name", a.id)

	req := &pb.GetDependencyRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetDependency(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Dependency %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *DependencyAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Dependency", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := APIHubDependencySpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateDependencyRequest{
		Parent:       a.id.ParentString(),
		DependencyId: a.id.Dependency,
		Dependency:   resource,
	}
	created, err := a.gcpClient.CreateDependency(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Dependency %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Dependency", "name", a.id)

	status := &krm.APIHubDependencyStatus{}
	status.ObservedState = APIHubDependencyObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *DependencyAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Dependency", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := APIHubDependencySpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareDependency(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateDependencyRequest{
			Dependency: desired,
			UpdateMask: updateMask,
		}
		updated, err := a.gcpClient.UpdateDependency(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Dependency %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Dependency", "name", a.id)

		status := &krm.APIHubDependencyStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = APIHubDependencyObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareDependency(ctx context.Context, actual, desired *pb.Dependency) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, APIHubDependencySpec_FromProto, APIHubDependencySpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *DependencyAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.APIHubDependency{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(APIHubDependencySpec_FromProto(mapCtx, a.actual))
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
	u.SetName(a.id.Dependency)
	u.SetGroupVersionKind(krm.APIHubDependencyGVK)
	return u, nil
}

func (a *DependencyAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Dependency", "name", a.id)

	req := &pb.DeleteDependencyRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteDependency(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Dependency %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Dependency", "name", a.id)
	return true, nil
}
