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

package siptrunk

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

	gcp "cloud.google.com/go/dialogflow/apiv2beta1"
	pb "cloud.google.com/go/dialogflow/apiv2beta1/dialogflowpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.DialogflowSipTrunkGVK, NewSipTrunkModel)
}

func NewSipTrunkModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelSipTrunk{config: *config}, nil
}

var _ directbase.Model = &modelSipTrunk{}
var _ = strings.TrimPrefix

type modelSipTrunk struct {
	config config.ControllerConfig
}

func (m *modelSipTrunk) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.DialogflowSipTrunk{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.DialogflowSipTrunkIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &SipTrunkAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelSipTrunk) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.DialogflowSipTrunkIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &SipTrunkAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelSipTrunk) client(ctx context.Context) (*gcp.SipTrunksClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewSipTrunksRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building SipTrunk client: %w", err)
	}
	return gcpClient, err
}

type SipTrunkAdapter struct {
	id        *krm.DialogflowSipTrunkIdentity
	gcpClient *gcp.SipTrunksClient
	desired   *krm.DialogflowSipTrunk
	actual    *pb.SipTrunk
}

var _ directbase.Adapter = &SipTrunkAdapter{}

func (a *SipTrunkAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting SipTrunk", "name", a.id)

	req := &pb.GetSipTrunkRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetSipTrunk(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting SipTrunk %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *SipTrunkAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating SipTrunk", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := DialogflowSipTrunkSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateSipTrunkRequest{
		Parent:     a.id.ParentString(),
		SipTrunkId: a.id.Siptrunk,
		SipTrunk:   resource,
	}
	created, err := a.gcpClient.CreateSipTrunk(ctx, req)
	if err != nil {
		return fmt.Errorf("creating SipTrunk %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created SipTrunk", "name", a.id)

	status := &krm.DialogflowSipTrunkStatus{}
	status.ObservedState = DialogflowSipTrunkObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *SipTrunkAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating SipTrunk", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := DialogflowSipTrunkSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareSipTrunk(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateSipTrunkRequest{
			SipTrunk:   desired,
			UpdateMask: updateMask,
		}
		updated, err := a.gcpClient.UpdateSipTrunk(ctx, req)
		if err != nil {
			return fmt.Errorf("updating SipTrunk %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated SipTrunk", "name", a.id)

		status := &krm.DialogflowSipTrunkStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = DialogflowSipTrunkObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareSipTrunk(ctx context.Context, actual, desired *pb.SipTrunk) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, DialogflowSipTrunkSpec_FromProto, DialogflowSipTrunkSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *SipTrunkAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.DialogflowSipTrunk{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(DialogflowSipTrunkSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = a.id.Location
	obj.Spec.ResourceID = direct.LazyPtr(a.id.Siptrunk)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Siptrunk)
	u.SetGroupVersionKind(krm.DialogflowSipTrunkGVK)
	return u, nil
}

func (a *SipTrunkAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting SipTrunk", "name", a.id)

	req := &pb.DeleteSipTrunkRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteSipTrunk(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting SipTrunk %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted SipTrunk", "name", a.id)
	return true, nil
}
