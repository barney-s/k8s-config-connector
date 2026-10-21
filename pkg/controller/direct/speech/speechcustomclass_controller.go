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

package speech

import (
	"context"
	"fmt"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/speech/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/speech/apiv2"
	pb "cloud.google.com/go/speech/apiv2/speechpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.SpeechCustomClassGVK, NewCustomClassModel)
}

func NewCustomClassModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelCustomClass{config: *config}, nil
}

var _ directbase.Model = &modelCustomClass{}
var _ = strings.TrimPrefix

type modelCustomClass struct {
	config config.ControllerConfig
}

func (m *modelCustomClass) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.SpeechCustomClass{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.CustomClassIdentity)

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &CustomClassAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelCustomClass) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelCustomClass) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building CustomClass client: %w", err)
	}
	return gcpClient, err
}

type CustomClassAdapter struct {
	id        *krm.CustomClassIdentity
	gcpClient *gcp.Client
	desired   *krm.SpeechCustomClass
	actual    *pb.CustomClass
}

var _ directbase.Adapter = &CustomClassAdapter{}

func (a *CustomClassAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting CustomClass", "name", a.id)

	req := &pb.GetCustomClassRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetCustomClass(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting CustomClass %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *CustomClassAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating CustomClass", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := SpeechCustomClassSpec_v1beta1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateCustomClassRequest{
		Parent:        a.id.ParentString(),
		CustomClassId: a.id.CustomClass,
		CustomClass:   resource,
	}
	op, err := a.gcpClient.CreateCustomClass(ctx, req)
	if err != nil {
		return fmt.Errorf("creating CustomClass %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create CustomClass %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created CustomClass", "name", a.id)

	status := &krm.SpeechCustomClassStatus{}
	status.ObservedState = SpeechCustomClassObservedState_v1beta1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *CustomClassAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating CustomClass", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := SpeechCustomClassSpec_v1beta1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareCustomClass(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateCustomClassRequest{
			CustomClass: desired,
			UpdateMask:  updateMask,
		}
		op, err := a.gcpClient.UpdateCustomClass(ctx, req)
		if err != nil {
			return fmt.Errorf("updating CustomClass %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update CustomClass %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated CustomClass", "name", a.id)

		status := &krm.SpeechCustomClassStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = SpeechCustomClassObservedState_v1beta1_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareCustomClass(ctx context.Context, actual, desired *pb.CustomClass) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, SpeechCustomClassSpec_v1beta1_FromProto, SpeechCustomClassSpec_v1beta1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *CustomClassAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.SpeechCustomClass{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(SpeechCustomClassSpec_v1beta1_FromProto(mapCtx, a.actual))
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
	u.SetName(a.id.CustomClass)
	u.SetGroupVersionKind(krm.SpeechCustomClassGVK)
	return u, nil
}

func (a *CustomClassAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting CustomClass", "name", a.id)

	req := &pb.DeleteCustomClassRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteCustomClass(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting CustomClass %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted CustomClass", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete CustomClass %s: %w", a.id, err)
		}
	}
	return true, nil
}
