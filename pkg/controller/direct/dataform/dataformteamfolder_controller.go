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

package dataform

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/dataform/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/dataform/apiv1"
	dataformpb "cloud.google.com/go/dataform/apiv1/dataformpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.DataformTeamFolderGVK, NewTeamFolderModel)
}

func NewTeamFolderModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelTeamFolder{config: *config}, nil
}

var _ directbase.Model = &modelTeamFolder{}
var _ = strings.TrimPrefix

type modelTeamFolder struct {
	config config.ControllerConfig
}

func (m *modelTeamFolder) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.DataformTeamFolder{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.DataformTeamFolderIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &TeamFolderAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelTeamFolder) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelTeamFolder) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building TeamFolder client: %w", err)
	}
	return gcpClient, err
}

type TeamFolderAdapter struct {
	id        *krm.DataformTeamFolderIdentity
	gcpClient *gcp.Client
	desired   *krm.DataformTeamFolder
	actual    *dataformpb.TeamFolder
}

var _ directbase.Adapter = &TeamFolderAdapter{}

func (a *TeamFolderAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting TeamFolder", "name", a.id)

	req := &dataformpb.GetTeamFolderRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetTeamFolder(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting TeamFolder %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *TeamFolderAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating TeamFolder", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := DataformTeamFolderSpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &dataformpb.CreateTeamFolderRequest{
		Parent:       a.id.ParentString(),
		TeamFolderId: a.id.Team,
		TeamFolder:   resource,
	}
	created, err := a.gcpClient.CreateTeamFolder(ctx, req)
	if err != nil {
		return fmt.Errorf("creating TeamFolder %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created TeamFolder", "name", a.id)

	status := &krm.DataformTeamFolderStatus{}
	status.ObservedState = DataformTeamFolderObservedState_v1alpha1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *TeamFolderAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating TeamFolder", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := DataformTeamFolderSpec_v1alpha1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareTeamFolder(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &dataformpb.UpdateTeamFolderRequest{
			TeamFolder: desired,
			UpdateMask: updateMask,
		}
		updated, err := a.gcpClient.UpdateTeamFolder(ctx, req)
		if err != nil {
			return fmt.Errorf("updating TeamFolder %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated TeamFolder", "name", a.id)

		status := &krm.DataformTeamFolderStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = DataformTeamFolderObservedState_v1alpha1_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareTeamFolder(ctx context.Context, actual, desired *dataformpb.TeamFolder) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, DataformTeamFolderSpec_v1alpha1_FromProto, DataformTeamFolderSpec_v1alpha1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *TeamFolderAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.DataformTeamFolder{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(DataformTeamFolderSpec_v1alpha1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = a.id.Location
	obj.Spec.ResourceID = direct.LazyPtr(a.id.Team)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Team)
	u.SetGroupVersionKind(krm.DataformTeamFolderGVK)
	return u, nil
}

func (a *TeamFolderAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting TeamFolder", "name", a.id)

	req := &dataformpb.DeleteTeamFolderRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteTeamFolder(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting TeamFolder %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted TeamFolder", "name", a.id)
	return true, nil
}
