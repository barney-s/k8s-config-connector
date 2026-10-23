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

package vectorsearch

import (
	"context"
	"fmt"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/vectorsearch/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/vectorsearch/apiv1"
	pb "cloud.google.com/go/vectorsearch/apiv1/vectorsearchpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.VectorSearchCollectionGVK, NewCollectionModel)
}

func NewCollectionModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelCollection{config: *config}, nil
}

var _ directbase.Model = &modelCollection{}
var _ = strings.TrimPrefix

type modelCollection struct {
	config config.ControllerConfig
}

func (m *modelCollection) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.VectorSearchCollection{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.VectorSearchCollectionIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &CollectionAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelCollection) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelCollection) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Collection client: %w", err)
	}
	return gcpClient, err
}

type CollectionAdapter struct {
	id        *krm.VectorSearchCollectionIdentity
	gcpClient *gcp.Client
	desired   *krm.VectorSearchCollection
	actual    *pb.Collection
}

var _ directbase.Adapter = &CollectionAdapter{}

func (a *CollectionAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Collection", "name", a.id)

	req := &pb.GetCollectionRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetCollection(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Collection %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *CollectionAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Collection", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := VectorSearchCollectionSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateCollectionRequest{
		Parent:       a.id.ParentString(),
		CollectionId: a.id.Collection,
		Collection:   resource,
	}
	op, err := a.gcpClient.CreateCollection(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Collection %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create Collection %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Collection", "name", a.id)

	status := &krm.VectorSearchCollectionStatus{}
	status.ObservedState = VectorSearchCollectionObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *CollectionAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Collection", "name", a.id)

	desired, diffs, updateMask, err := compareCollection(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateCollectionRequest{
			Collection: desired,
			UpdateMask: updateMask,
		}
		op, err := a.gcpClient.UpdateCollection(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Collection %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update Collection %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Collection", "name", a.id)

		status := &krm.VectorSearchCollectionStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = VectorSearchCollectionObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareCollection(ctx context.Context, u *unstructured.Unstructured, actual *pb.Collection, desiredSpec *krm.VectorSearchCollectionSpec) (*pb.Collection, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		VectorSearchCollectionSpec_FromProto,
		VectorSearchCollectionSpec_ToProto,
		nil,
	)
}

func (a *CollectionAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.VectorSearchCollection{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(VectorSearchCollectionSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = &a.id.Location
	obj.Spec.ResourceID = &a.id.Collection

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Collection)
	u.SetGroupVersionKind(krm.VectorSearchCollectionGVK)
	return u, nil
}

func (a *CollectionAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Collection", "name", a.id)

	req := &pb.DeleteCollectionRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteCollection(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Collection %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Collection", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete Collection %s: %w", a.id, err)
		}
	}
	return true, nil
}
