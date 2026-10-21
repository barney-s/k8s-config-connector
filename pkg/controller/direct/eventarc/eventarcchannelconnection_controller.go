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

package eventarc

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/eventarc/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/eventarc/apiv1"
	pb "cloud.google.com/go/eventarc/apiv1/eventarcpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.EventarcChannelConnectionGVK, NewChannelConnectionModel)
}

func NewChannelConnectionModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelChannelConnection{config: *config}, nil
}

var _ directbase.Model = &modelChannelConnection{}
var _ = strings.TrimPrefix

type modelChannelConnection struct {
	config config.ControllerConfig
}

func (m *modelChannelConnection) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.EventarcChannelConnection{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.EventarcChannelConnectionIdentity{}
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ChannelConnectionAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelChannelConnection) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.EventarcChannelConnectionIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ChannelConnectionAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelChannelConnection) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ChannelConnection client: %w", err)
	}
	return gcpClient, err
}

type ChannelConnectionAdapter struct {
	id        *krm.EventarcChannelConnectionIdentity
	gcpClient *gcp.Client
	desired   *krm.EventarcChannelConnection
	actual    *pb.ChannelConnection
}

var _ directbase.Adapter = &ChannelConnectionAdapter{}

func (a *ChannelConnectionAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ChannelConnection", "name", a.id)

	req := &pb.GetChannelConnectionRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetChannelConnection(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ChannelConnection %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ChannelConnectionAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ChannelConnection", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := EventarcChannelConnectionSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateChannelConnectionRequest{
		Parent:              a.id.ParentString(),
		ChannelConnectionId: a.id.ChannelConnection,
		ChannelConnection:   resource,
	}
	op, err := a.gcpClient.CreateChannelConnection(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ChannelConnection %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create ChannelConnection %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ChannelConnection", "name", a.id)

	status := &krm.EventarcChannelConnectionStatus{}
	status.ObservedState = EventarcChannelConnectionObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ChannelConnectionAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ChannelConnection", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := EventarcChannelConnectionSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareChannelConnection(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("EventarcChannelConnection resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.EventarcChannelConnectionStatus{}
	mapCtx = &direct.MapContext{}
	status.ObservedState = EventarcChannelConnectionObservedState_FromProto(mapCtx, a.actual)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareChannelConnection(ctx context.Context, actual, desired *pb.ChannelConnection) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, EventarcChannelConnectionSpec_FromProto, EventarcChannelConnectionSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *ChannelConnectionAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.EventarcChannelConnection{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(EventarcChannelConnectionSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = a.id.Location
	obj.Spec.ResourceID = direct.LazyPtr(a.id.ChannelConnection)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ChannelConnection)
	u.SetGroupVersionKind(krm.EventarcChannelConnectionGVK)
	return u, nil
}

func (a *ChannelConnectionAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting ChannelConnection", "name", a.id)

	req := &pb.DeleteChannelConnectionRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteChannelConnection(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting ChannelConnection %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted ChannelConnection", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete ChannelConnection %s: %w", a.id, err)
		}
	}
	return true, nil
}
