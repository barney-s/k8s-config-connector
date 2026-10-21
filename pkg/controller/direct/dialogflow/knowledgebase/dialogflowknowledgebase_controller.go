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

package knowledgebase

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/dialogflow/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/dialogflow/apiv2"
	pb "cloud.google.com/go/dialogflow/apiv2/dialogflowpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.DialogflowKnowledgeBaseGVK, NewKnowledgeBaseModel)
}

func NewKnowledgeBaseModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelKnowledgeBase{config: *config}, nil
}

var _ directbase.Model = &modelKnowledgeBase{}
var _ = strings.TrimPrefix

type modelKnowledgeBase struct {
	config config.ControllerConfig
}

func (m *modelKnowledgeBase) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.DialogflowKnowledgeBase{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.DialogflowKnowledgeBaseIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &KnowledgeBaseAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelKnowledgeBase) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.DialogflowKnowledgeBaseIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &KnowledgeBaseAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelKnowledgeBase) client(ctx context.Context) (*gcp.KnowledgeBasesClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewKnowledgeBasesRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building KnowledgeBase client: %w", err)
	}
	return gcpClient, err
}

type KnowledgeBaseAdapter struct {
	id        *krm.DialogflowKnowledgeBaseIdentity
	gcpClient *gcp.KnowledgeBasesClient
	desired   *krm.DialogflowKnowledgeBase
	actual    *pb.KnowledgeBase
}

var _ directbase.Adapter = &KnowledgeBaseAdapter{}

func (a *KnowledgeBaseAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting KnowledgeBase", "name", a.id)

	req := &pb.GetKnowledgeBaseRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetKnowledgeBase(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting KnowledgeBase %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *KnowledgeBaseAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating KnowledgeBase", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := DialogflowKnowledgeBaseSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateKnowledgeBaseRequest{
		Parent:          a.id.ParentString(),
		KnowledgeBaseId: a.id.Knowledge,
		KnowledgeBase:   resource,
	}
	created, err := a.gcpClient.CreateKnowledgeBase(ctx, req)
	if err != nil {
		return fmt.Errorf("creating KnowledgeBase %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created KnowledgeBase", "name", a.id)

	status := &krm.DialogflowKnowledgeBaseStatus{}
	status.ObservedState = DialogflowKnowledgeBaseObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *KnowledgeBaseAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating KnowledgeBase", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := DialogflowKnowledgeBaseSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareKnowledgeBase(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateKnowledgeBaseRequest{
			KnowledgeBase: desired,
			UpdateMask:    updateMask,
		}
		updated, err := a.gcpClient.UpdateKnowledgeBase(ctx, req)
		if err != nil {
			return fmt.Errorf("updating KnowledgeBase %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated KnowledgeBase", "name", a.id)

		status := &krm.DialogflowKnowledgeBaseStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = DialogflowKnowledgeBaseObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareKnowledgeBase(ctx context.Context, actual, desired *pb.KnowledgeBase) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, DialogflowKnowledgeBaseSpec_FromProto, DialogflowKnowledgeBaseSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *KnowledgeBaseAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.DialogflowKnowledgeBase{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(DialogflowKnowledgeBaseSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = a.id.Location
	obj.Spec.ResourceID = direct.LazyPtr(a.id.Knowledge)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Knowledge)
	u.SetGroupVersionKind(krm.DialogflowKnowledgeBaseGVK)
	return u, nil
}

func (a *KnowledgeBaseAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting KnowledgeBase", "name", a.id)

	req := &pb.DeleteKnowledgeBaseRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteKnowledgeBase(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting KnowledgeBase %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted KnowledgeBase", "name", a.id)
	return true, nil
}
