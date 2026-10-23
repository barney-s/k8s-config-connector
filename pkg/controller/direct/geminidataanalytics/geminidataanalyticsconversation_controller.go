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

package geminidataanalytics

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/geminidataanalytics/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/geminidataanalytics/apiv1beta"
	pb "cloud.google.com/go/geminidataanalytics/apiv1beta/geminidataanalyticspb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.GeminiDataAnalyticsConversationGVK, NewConversationModel)
}

func NewConversationModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelConversation{config: *config}, nil
}

var _ directbase.Model = &modelConversation{}
var _ = strings.TrimPrefix

type modelConversation struct {
	config config.ControllerConfig
}

func (m *modelConversation) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.GeminiDataAnalyticsConversation{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id, err := krm.NewGeminiDataAnalyticsConversationIdentity(ctx, reader, obj)
	if err != nil {
		return nil, err
	}
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ConversationAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelConversation) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelConversation) client(ctx context.Context) (*gcp.DataChatClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewDataChatRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Conversation client: %w", err)
	}
	return gcpClient, err
}

type ConversationAdapter struct {
	id        *krm.GeminiDataAnalyticsConversationIdentity
	gcpClient *gcp.DataChatClient
	desired   *krm.GeminiDataAnalyticsConversation
	actual    *pb.Conversation
}

var _ directbase.Adapter = &ConversationAdapter{}

func (a *ConversationAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Conversation", "name", a.id)

	req := &pb.GetConversationRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetConversation(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Conversation %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ConversationAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Conversation", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := GeminiDataAnalyticsConversationSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateConversationRequest{
		Parent:         a.id.Parent().String(),
		ConversationId: a.id.ID(),
		Conversation:   resource,
	}
	created, err := a.gcpClient.CreateConversation(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Conversation %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Conversation", "name", a.id)

	status := &krm.GeminiDataAnalyticsConversationStatus{}
	status.ObservedState = GeminiDataAnalyticsConversationObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ConversationAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Conversation", "name", a.id)

	desired, diffs, updateMask, err := compareConversation(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	_ = desired
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("GeminiDataAnalyticsConversation resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.GeminiDataAnalyticsConversationStatus{}
	mapCtx := &direct.MapContext{}
	status.ObservedState = GeminiDataAnalyticsConversationObservedState_FromProto(mapCtx, a.actual)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareConversation(ctx context.Context, u *unstructured.Unstructured, actual *pb.Conversation, desiredSpec *krm.GeminiDataAnalyticsConversationSpec) (*pb.Conversation, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		GeminiDataAnalyticsConversationSpec_FromProto,
		GeminiDataAnalyticsConversationSpec_ToProto,
		nil,
	)
}

func (a *ConversationAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.GeminiDataAnalyticsConversation{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(GeminiDataAnalyticsConversationSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{Name: a.id.Parent().ProjectID}
	obj.Spec.Location = a.id.Parent().Location

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.GeminiDataAnalyticsConversationGVK)
	return u, nil
}

func (a *ConversationAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Conversation", "name", a.id)

	req := &pb.DeleteConversationRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteConversation(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Conversation %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Conversation", "name", a.id)
	return true, nil
}
