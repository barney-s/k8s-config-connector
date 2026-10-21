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

package firestore

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/firestore/v1beta1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/firestore/apiv1/admin"
	pb "cloud.google.com/go/firestore/apiv1/admin/adminpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.FirestoreDatabaseGVK, NewFirestoreDatabaseModel)
}

func NewFirestoreDatabaseModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelFirestoreDatabase{config: *config}, nil
}

var _ directbase.Model = &modelFirestoreDatabase{}
var _ = strings.TrimPrefix

type modelFirestoreDatabase struct {
	config config.ControllerConfig
}

func (m *modelFirestoreDatabase) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.FirestoreDatabase{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.FirestoreDatabaseIdentity)

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &FirestoreDatabaseAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelFirestoreDatabase) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.FirestoreDatabaseIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &FirestoreDatabaseAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelFirestoreDatabase) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building FirestoreDatabase client: %w", err)
	}
	return gcpClient, err
}

type FirestoreDatabaseAdapter struct {
	id        *krm.FirestoreDatabaseIdentity
	gcpClient *gcp.Client
	desired   *krm.FirestoreDatabase
	actual    *pb.FirestoreDatabase
}

var _ directbase.Adapter = &FirestoreDatabaseAdapter{}

func (a *FirestoreDatabaseAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting FirestoreDatabase", "name", a.id)

	req := &pb.GetFirestoreDatabaseRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetFirestoreDatabase(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting FirestoreDatabase %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *FirestoreDatabaseAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating FirestoreDatabase", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := FirestoreDatabaseSpec_v1beta1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateFirestoreDatabaseRequest{
		Parent:            a.id.ParentString(),
		DatabaseId:        a.id.Database,
		FirestoreDatabase: resource,
	}
	op, err := a.gcpClient.CreateFirestoreDatabase(ctx, req)
	if err != nil {
		return fmt.Errorf("creating FirestoreDatabase %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create FirestoreDatabase %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created FirestoreDatabase", "name", a.id)

	status := &krm.FirestoreDatabaseStatus{}
	status.ObservedState = FirestoreDatabaseObservedState_v1beta1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *FirestoreDatabaseAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating FirestoreDatabase", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := FirestoreDatabaseSpec_v1beta1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareFirestoreDatabase(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("FirestoreDatabase resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.FirestoreDatabaseStatus{}
	mapCtx = &direct.MapContext{}
	status.ObservedState = FirestoreDatabaseObservedState_v1beta1_FromProto(mapCtx, a.actual)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareFirestoreDatabase(ctx context.Context, actual, desired *pb.FirestoreDatabase) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, FirestoreDatabaseSpec_v1beta1_FromProto, FirestoreDatabaseSpec_v1beta1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *FirestoreDatabaseAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.FirestoreDatabase{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(FirestoreDatabaseSpec_v1beta1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Database)
	u.SetGroupVersionKind(krm.FirestoreDatabaseGVK)
	return u, nil
}

func (a *FirestoreDatabaseAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deletion not supported for FirestoreDatabase, removing finalizer without deleting GCP resource", "name", a.id)
	return true, nil
}
