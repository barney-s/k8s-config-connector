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

package storagecontrol

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/storage/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/storage/control/apiv2"
	pb "cloud.google.com/go/storage/control/apiv2/controlpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.StorageAnywhereCacheGVK, NewAnywhereCacheModel)
}

func NewAnywhereCacheModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelAnywhereCache{config: *config}, nil
}

var _ directbase.Model = &modelAnywhereCache{}
var _ = strings.TrimPrefix

type modelAnywhereCache struct {
	config config.ControllerConfig
}

func (m *modelAnywhereCache) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.StorageAnywhereCache{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.AnywhereCacheIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &AnywhereCacheAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelAnywhereCache) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelAnywhereCache) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.GRPCClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewStorageControlClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building AnywhereCache client: %w", err)
	}
	return gcpClient, err
}

type AnywhereCacheAdapter struct {
	id        *krm.AnywhereCacheIdentity
	gcpClient *gcp.Client
	desired   *krm.StorageAnywhereCache
	actual    *pb.AnywhereCache
}

var _ directbase.Adapter = &AnywhereCacheAdapter{}

func (a *AnywhereCacheAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting AnywhereCache", "name", a.id)

	req := &pb.GetAnywhereCacheRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetAnywhereCache(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting AnywhereCache %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *AnywhereCacheAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating AnywhereCache", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := StorageAnywhereCacheSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateAnywhereCacheRequest{
		Parent:          a.id.Parent().String(),
		AnywhereCacheId: a.id.ID(),
		AnywhereCache:   resource,
	}
	op, err := a.gcpClient.CreateAnywhereCache(ctx, req)
	if err != nil {
		return fmt.Errorf("creating AnywhereCache %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create AnywhereCache %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created AnywhereCache", "name", a.id)

	status := &krm.StorageAnywhereCacheStatus{}
	status.ObservedState = StorageAnywhereCacheObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *AnywhereCacheAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating AnywhereCache", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := StorageAnywhereCacheSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareAnywhereCache(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateAnywhereCacheRequest{
			AnywhereCache: desired,
			UpdateMask:    updateMask,
		}
		op, err := a.gcpClient.UpdateAnywhereCache(ctx, req)
		if err != nil {
			return fmt.Errorf("updating AnywhereCache %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update AnywhereCache %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated AnywhereCache", "name", a.id)

		status := &krm.StorageAnywhereCacheStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = StorageAnywhereCacheObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareAnywhereCache(ctx context.Context, actual, desired *pb.AnywhereCache) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, StorageAnywhereCacheSpec_FromProto, StorageAnywhereCacheSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *AnywhereCacheAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.StorageAnywhereCache{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(StorageAnywhereCacheSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ResourceID = direct.LazyPtr(a.id.ID())

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.StorageAnywhereCacheGVK)
	return u, nil
}

func (a *AnywhereCacheAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deletion not supported for AnywhereCache, removing finalizer without deleting GCP resource", "name", a.id)
	return true, nil
}
