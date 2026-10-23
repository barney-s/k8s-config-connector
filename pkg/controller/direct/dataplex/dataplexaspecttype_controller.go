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

package dataplex

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/dataplex/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/dataplex/apiv1"
	pb "cloud.google.com/go/dataplex/apiv1/dataplexpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.DataplexAspectTypeGVK, NewAspectTypeModel)
}

func NewAspectTypeModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelAspectType{config: *config}, nil
}

var _ directbase.Model = &modelAspectType{}
var _ = strings.TrimPrefix

type modelAspectType struct {
	config config.ControllerConfig
}

func (m *modelAspectType) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.DataplexAspectType{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.AspectTypeIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &AspectTypeAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelAspectType) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelAspectType) client(ctx context.Context) (*gcp.CatalogClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewCatalogRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building AspectType client: %w", err)
	}
	return gcpClient, err
}

type AspectTypeAdapter struct {
	id        *krm.AspectTypeIdentity
	gcpClient *gcp.CatalogClient
	desired   *krm.DataplexAspectType
	actual    *pb.AspectType
}

var _ directbase.Adapter = &AspectTypeAdapter{}

func (a *AspectTypeAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting AspectType", "name", a.id)

	req := &pb.GetAspectTypeRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetAspectType(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting AspectType %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *AspectTypeAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating AspectType", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := DataplexAspectTypeSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateAspectTypeRequest{
		Parent:       a.id.ParentString(),
		AspectTypeId: a.id.AspectType,
		AspectType:   resource,
	}
	op, err := a.gcpClient.CreateAspectType(ctx, req)
	if err != nil {
		return fmt.Errorf("creating AspectType %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create AspectType %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created AspectType", "name", a.id)

	status := &krm.DataplexAspectTypeStatus{}
	status.ObservedState = DataplexAspectTypeObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *AspectTypeAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating AspectType", "name", a.id)

	desired, diffs, updateMask, err := compareAspectType(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateAspectTypeRequest{
			AspectType: desired,
			UpdateMask: updateMask,
		}
		op, err := a.gcpClient.UpdateAspectType(ctx, req)
		if err != nil {
			return fmt.Errorf("updating AspectType %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update AspectType %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated AspectType", "name", a.id)

		status := &krm.DataplexAspectTypeStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = DataplexAspectTypeObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareAspectType(ctx context.Context, u *unstructured.Unstructured, actual *pb.AspectType, desiredSpec *krm.DataplexAspectTypeSpec) (*pb.AspectType, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		DataplexAspectTypeSpec_FromProto,
		DataplexAspectTypeSpec_ToProto,
		nil,
	)
}

func (a *AspectTypeAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.DataplexAspectType{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(DataplexAspectTypeSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.AspectType)
	u.SetGroupVersionKind(krm.DataplexAspectTypeGVK)
	return u, nil
}

func (a *AspectTypeAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting AspectType", "name", a.id)

	req := &pb.DeleteAspectTypeRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteAspectType(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting AspectType %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted AspectType", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete AspectType %s: %w", a.id, err)
		}
	}
	return true, nil
}
