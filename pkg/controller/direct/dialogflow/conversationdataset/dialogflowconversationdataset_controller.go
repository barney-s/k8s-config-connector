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

package conversationdataset

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
	registry.RegisterModel(krm.DialogflowConversationDatasetGVK, NewConversationDatasetModel)
}

func NewConversationDatasetModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelConversationDataset{config: *config}, nil
}

var _ directbase.Model = &modelConversationDataset{}
var _ = strings.TrimPrefix

type modelConversationDataset struct {
	config config.ControllerConfig
}

func (m *modelConversationDataset) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.DialogflowConversationDataset{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.DialogflowConversationDatasetIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ConversationDatasetAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelConversationDataset) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.DialogflowConversationDatasetIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ConversationDatasetAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelConversationDataset) client(ctx context.Context) (*gcp.ConversationDatasetsClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewConversationDatasetsRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ConversationDataset client: %w", err)
	}
	return gcpClient, err
}

type ConversationDatasetAdapter struct {
	id        *krm.DialogflowConversationDatasetIdentity
	gcpClient *gcp.ConversationDatasetsClient
	desired   *krm.DialogflowConversationDataset
	actual    *pb.ConversationDataset
}

var _ directbase.Adapter = &ConversationDatasetAdapter{}

func (a *ConversationDatasetAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ConversationDataset", "name", a.id)
	if a.id.ConversationDataset == "" {
		return false, nil
	}

	req := &pb.GetConversationDatasetRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetConversationDataset(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ConversationDataset %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ConversationDatasetAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ConversationDataset", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := DialogflowConversationDatasetSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateConversationDatasetRequest{
		Parent:              a.id.ParentString(),
		ConversationDataset: resource,
	}
	op, err := a.gcpClient.CreateConversationDataset(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ConversationDataset %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create ConversationDataset %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ConversationDataset", "name", a.id)

	status := &krm.DialogflowConversationDatasetStatus{}
	status.ObservedState = DialogflowConversationDatasetObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(created.GetName())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ConversationDatasetAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ConversationDataset", "name", a.id)

	desired, diffs, updateMask, err := compareConversationDataset(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	_ = desired
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("DialogflowConversationDataset resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.DialogflowConversationDatasetStatus{}
	mapCtx := &direct.MapContext{}
	status.ObservedState = DialogflowConversationDatasetObservedState_FromProto(mapCtx, a.actual)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareConversationDataset(ctx context.Context, u *unstructured.Unstructured, actual *pb.ConversationDataset, desiredSpec *krm.DialogflowConversationDatasetSpec) (*pb.ConversationDataset, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		DialogflowConversationDatasetSpec_FromProto,
		DialogflowConversationDatasetSpec_ToProto,
		nil,
	)
}

func (a *ConversationDatasetAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.DialogflowConversationDataset{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(DialogflowConversationDatasetSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{Name: a.id.Project}
	obj.Spec.Location = &a.id.Location
	obj.Spec.ResourceID = direct.LazyPtr(a.id.ConversationDataset)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ConversationDataset)
	u.SetGroupVersionKind(krm.DialogflowConversationDatasetGVK)
	return u, nil
}

func (a *ConversationDatasetAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting ConversationDataset", "name", a.id)

	req := &pb.DeleteConversationDatasetRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteConversationDataset(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting ConversationDataset %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted ConversationDataset", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete ConversationDataset %s: %w", a.id, err)
		}
	}
	return true, nil
}
