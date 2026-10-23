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

package livestream

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/livestream/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/video/livestream/apiv1"
	pb "cloud.google.com/go/video/livestream/apiv1/livestreampb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.LiveStreamChannelGVK, NewChannelModel)
}

func NewChannelModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelChannel{config: *config}, nil
}

var _ directbase.Model = &modelChannel{}
var _ = strings.TrimPrefix

type modelChannel struct {
	config config.ControllerConfig
}

func (m *modelChannel) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.LiveStreamChannel{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.LiveStreamChannelIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ChannelAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelChannel) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.LiveStreamChannelIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ChannelAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelChannel) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Channel client: %w", err)
	}
	return gcpClient, err
}

type ChannelAdapter struct {
	id        *krm.LiveStreamChannelIdentity
	gcpClient *gcp.Client
	desired   *krm.LiveStreamChannel
	actual    *pb.Channel
}

var _ directbase.Adapter = &ChannelAdapter{}

func (a *ChannelAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Channel", "name", a.id)

	req := &pb.GetChannelRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetChannel(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Channel %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ChannelAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Channel", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := LiveStreamChannelSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateChannelRequest{
		Parent:    a.id.ParentString(),
		ChannelId: a.id.Channel,
		Channel:   resource,
	}
	op, err := a.gcpClient.CreateChannel(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Channel %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create Channel %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Channel", "name", a.id)

	status := &krm.LiveStreamChannelStatus{}
	status.ObservedState = LiveStreamChannelObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ChannelAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Channel", "name", a.id)

	desired, diffs, updateMask, err := compareChannel(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateChannelRequest{
			Channel:    desired,
			UpdateMask: updateMask,
		}
		op, err := a.gcpClient.UpdateChannel(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Channel %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update Channel %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Channel", "name", a.id)

		status := &krm.LiveStreamChannelStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = LiveStreamChannelObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareChannel(ctx context.Context, u *unstructured.Unstructured, actual *pb.Channel, desiredSpec *krm.LiveStreamChannelSpec) (*pb.Channel, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		LiveStreamChannelSpec_FromProto,
		LiveStreamChannelSpec_ToProto,
		nil,
	)
}

func (a *ChannelAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.LiveStreamChannel{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(LiveStreamChannelSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = direct.LazyPtr(a.id.Location)
	obj.Spec.ResourceID = direct.LazyPtr(a.id.Channel)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Channel)
	u.SetGroupVersionKind(krm.LiveStreamChannelGVK)
	return u, nil
}

func (a *ChannelAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Channel", "name", a.id)

	req := &pb.DeleteChannelRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteChannel(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Channel %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Channel", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete Channel %s: %w", a.id, err)
		}
	}
	return true, nil
}
