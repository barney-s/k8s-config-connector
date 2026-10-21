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

package bigtable

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/bigtable/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/bigtable"
	bigtablepb "cloud.google.com/go/bigtable/admin/apiv2/adminpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.BigtableAppProfileGVK, NewAppProfileModel)
}

func NewAppProfileModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelAppProfile{config: *config}, nil
}

var _ directbase.Model = &modelAppProfile{}
var _ = strings.TrimPrefix

type modelAppProfile struct {
	config config.ControllerConfig
}

func (m *modelAppProfile) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.BigtableAppProfile{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.BigtableAppProfileIdentity)

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &AppProfileAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelAppProfile) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelAppProfile) client(ctx context.Context) (*gcp.InstanceAdminClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.GRPCClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewInstanceAdminClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building AppProfile client: %w", err)
	}
	return gcpClient, err
}

type AppProfileAdapter struct {
	id        *krm.BigtableAppProfileIdentity
	gcpClient *gcp.InstanceAdminClient
	desired   *krm.BigtableAppProfile
	actual    *bigtablepb.AppProfile
}

var _ directbase.Adapter = &AppProfileAdapter{}

func (a *AppProfileAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting AppProfile", "name", a.id)

	req := &bigtablepb.GetAppProfileRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetAppProfile(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting AppProfile %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *AppProfileAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating AppProfile", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := BigtableAppProfileSpec_v1beta1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &bigtablepb.CreateAppProfileRequest{
		Parent:       a.id.ParentString(),
		AppProfileId: a.id.Instance,
		AppProfile:   resource,
	}
	created, err := a.gcpClient.CreateAppProfile(ctx, req)
	if err != nil {
		return fmt.Errorf("creating AppProfile %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created AppProfile", "name", a.id)

	status := &krm.BigtableAppProfileStatus{}
	status.ObservedState = BigtableAppProfileObservedState_v1beta1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *AppProfileAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating AppProfile", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := BigtableAppProfileSpec_v1beta1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareAppProfile(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &bigtablepb.UpdateAppProfileRequest{
			AppProfile: desired,
		}
		updated, err := a.gcpClient.UpdateAppProfile(ctx, req)
		if err != nil {
			return fmt.Errorf("updating AppProfile %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated AppProfile", "name", a.id)

		status := &krm.BigtableAppProfileStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = BigtableAppProfileObservedState_v1beta1_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareAppProfile(ctx context.Context, actual, desired *bigtablepb.AppProfile) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, BigtableAppProfileSpec_v1beta1_FromProto, BigtableAppProfileSpec_v1beta1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *AppProfileAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.BigtableAppProfile{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(BigtableAppProfileSpec_v1beta1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Instance)
	u.SetGroupVersionKind(krm.BigtableAppProfileGVK)
	return u, nil
}

func (a *AppProfileAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting AppProfile", "name", a.id)

	req := &bigtablepb.DeleteAppProfileRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteAppProfile(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting AppProfile %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted AppProfile", "name", a.id)
	return true, nil
}
