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

package cloudnumberregistry

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/cloudnumberregistry/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/longrunning/autogen"
	longrunningpb "cloud.google.com/go/longrunning/autogen/longrunningpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.CloudNumberRegistryRegistryBookGVK, NewRegistryBookModel)
}

func NewRegistryBookModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelRegistryBook{config: *config}, nil
}

var _ directbase.Model = &modelRegistryBook{}
var _ = strings.TrimPrefix

type modelRegistryBook struct {
	config config.ControllerConfig
}

func (m *modelRegistryBook) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.CloudNumberRegistryRegistryBook{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.CloudNumberRegistryRegistryBookIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &RegistryBookAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelRegistryBook) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelRegistryBook) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.GRPCClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building RegistryBook client: %w", err)
	}
	return gcpClient, err
}

type RegistryBookAdapter struct {
	id        *krm.CloudNumberRegistryRegistryBookIdentity
	gcpClient *gcp.Client
	desired   *krm.CloudNumberRegistryRegistryBook
	actual    *longrunningpb.RegistryBook
}

var _ directbase.Adapter = &RegistryBookAdapter{}

func (a *RegistryBookAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting RegistryBook", "name", a.id)

	req := &longrunningpb.GetRegistryBookRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetRegistryBook(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting RegistryBook %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *RegistryBookAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating RegistryBook", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := CloudNumberRegistryRegistryBookSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &longrunningpb.CreateRegistryBookRequest{
		Parent:         a.id.ParentString(),
		RegistryBookId: a.id.RegistryBook,
		RegistryBook:   resource,
	}
	op, err := a.gcpClient.CreateRegistryBook(ctx, req)
	if err != nil {
		return fmt.Errorf("creating RegistryBook %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create RegistryBook %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created RegistryBook", "name", a.id)

	status := &krm.CloudNumberRegistryRegistryBookStatus{}
	status.ObservedState = CloudNumberRegistryRegistryBookObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *RegistryBookAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating RegistryBook", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := CloudNumberRegistryRegistryBookSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareRegistryBook(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &longrunningpb.UpdateRegistryBookRequest{
			RegistryBook: desired,
			UpdateMask:   updateMask,
		}
		op, err := a.gcpClient.UpdateRegistryBook(ctx, req)
		if err != nil {
			return fmt.Errorf("updating RegistryBook %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update RegistryBook %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated RegistryBook", "name", a.id)

		status := &krm.CloudNumberRegistryRegistryBookStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = CloudNumberRegistryRegistryBookObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareRegistryBook(ctx context.Context, actual, desired *longrunningpb.RegistryBook) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, CloudNumberRegistryRegistryBookSpec_FromProto, CloudNumberRegistryRegistryBookSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *RegistryBookAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.CloudNumberRegistryRegistryBook{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(CloudNumberRegistryRegistryBookSpec_FromProto(mapCtx, a.actual))
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
	u.SetName(a.id.RegistryBook)
	u.SetGroupVersionKind(krm.CloudNumberRegistryRegistryBookGVK)
	return u, nil
}

func (a *RegistryBookAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting RegistryBook", "name", a.id)

	req := &longrunningpb.DeleteRegistryBookRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteRegistryBook(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting RegistryBook %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted RegistryBook", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete RegistryBook %s: %w", a.id, err)
		}
	}
	return true, nil
}
