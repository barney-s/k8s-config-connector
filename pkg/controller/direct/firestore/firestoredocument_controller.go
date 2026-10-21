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
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/firestore/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/firestore/apiv1"
	pb "cloud.google.com/go/firestore/apiv1/firestorepb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.FirestoreDocumentGVK, NewFirestoreDocumentModel)
}

func NewFirestoreDocumentModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelFirestoreDocument{config: *config}, nil
}

var _ directbase.Model = &modelFirestoreDocument{}
var _ = strings.TrimPrefix

type modelFirestoreDocument struct {
	config config.ControllerConfig
}

func (m *modelFirestoreDocument) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.FirestoreDocument{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.FirestoreDocumentIdentity)

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &FirestoreDocumentAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelFirestoreDocument) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.FirestoreDocumentIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &FirestoreDocumentAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelFirestoreDocument) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building FirestoreDocument client: %w", err)
	}
	return gcpClient, err
}

type FirestoreDocumentAdapter struct {
	id        *krm.FirestoreDocumentIdentity
	gcpClient *gcp.Client
	desired   *krm.FirestoreDocument
	actual    *pb.FirestoreDocument
}

var _ directbase.Adapter = &FirestoreDocumentAdapter{}

func (a *FirestoreDocumentAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting FirestoreDocument", "name", a.id)

	req := &pb.GetFirestoreDocumentRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetFirestoreDocument(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting FirestoreDocument %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *FirestoreDocumentAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating FirestoreDocument", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := FirestoreDocumentSpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateFirestoreDocumentRequest{
		Parent:            a.id.ParentString(),
		CollectionId:      a.id.Collection,
		FirestoreDocument: resource,
	}
	created, err := a.gcpClient.CreateFirestoreDocument(ctx, req)
	if err != nil {
		return fmt.Errorf("creating FirestoreDocument %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created FirestoreDocument", "name", a.id)

	status := &krm.FirestoreDocumentStatus{}
	status.ObservedState = FirestoreDocumentObservedState_v1alpha1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *FirestoreDocumentAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating FirestoreDocument", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := FirestoreDocumentSpec_v1alpha1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareFirestoreDocument(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("FirestoreDocument resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.FirestoreDocumentStatus{}
	mapCtx = &direct.MapContext{}
	status.ObservedState = FirestoreDocumentObservedState_v1alpha1_FromProto(mapCtx, a.actual)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareFirestoreDocument(ctx context.Context, actual, desired *pb.FirestoreDocument) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, FirestoreDocumentSpec_v1alpha1_FromProto, FirestoreDocumentSpec_v1alpha1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *FirestoreDocumentAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.FirestoreDocument{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(FirestoreDocumentSpec_v1alpha1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Collection)
	u.SetGroupVersionKind(krm.FirestoreDocumentGVK)
	return u, nil
}

func (a *FirestoreDocumentAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deletion not supported for FirestoreDocument, removing finalizer without deleting GCP resource", "name", a.id)
	return true, nil
}
