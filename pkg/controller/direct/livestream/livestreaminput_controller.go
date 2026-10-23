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
	registry.RegisterModel(krm.LiveStreamInputGVK, NewInputModel)
}

func NewInputModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelInput{config: *config}, nil
}

var _ directbase.Model = &modelInput{}
var _ = strings.TrimPrefix

type modelInput struct {
	config config.ControllerConfig
}

func (m *modelInput) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.LiveStreamInput{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.LiveStreamInputIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &InputAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelInput) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.LiveStreamInputIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &InputAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelInput) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Input client: %w", err)
	}
	return gcpClient, err
}

type InputAdapter struct {
	id        *krm.LiveStreamInputIdentity
	gcpClient *gcp.Client
	desired   *krm.LiveStreamInput
	actual    *pb.Input
}

var _ directbase.Adapter = &InputAdapter{}

func (a *InputAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Input", "name", a.id)

	req := &pb.GetInputRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetInput(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Input %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *InputAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Input", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := LiveStreamInputSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateInputRequest{
		Parent:  a.id.ParentString(),
		InputId: a.id.Input,
		Input:   resource,
	}
	op, err := a.gcpClient.CreateInput(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Input %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create Input %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Input", "name", a.id)

	status := &krm.LiveStreamInputStatus{}
	status.ObservedState = LiveStreamInputObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *InputAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Input", "name", a.id)

	desired, diffs, updateMask, err := compareInput(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateInputRequest{
			Input:      desired,
			UpdateMask: updateMask,
		}
		op, err := a.gcpClient.UpdateInput(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Input %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update Input %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Input", "name", a.id)

		status := &krm.LiveStreamInputStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = LiveStreamInputObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareInput(ctx context.Context, u *unstructured.Unstructured, actual *pb.Input, desiredSpec *krm.LiveStreamInputSpec) (*pb.Input, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		LiveStreamInputSpec_FromProto,
		LiveStreamInputSpec_ToProto,
		nil,
	)
}

func (a *InputAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.LiveStreamInput{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(LiveStreamInputSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = direct.LazyPtr(a.id.Location)
	obj.Spec.ResourceID = direct.LazyPtr(a.id.Input)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Input)
	u.SetGroupVersionKind(krm.LiveStreamInputGVK)
	return u, nil
}

func (a *InputAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Input", "name", a.id)

	req := &pb.DeleteInputRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteInput(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Input %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Input", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete Input %s: %w", a.id, err)
		}
	}
	return true, nil
}
