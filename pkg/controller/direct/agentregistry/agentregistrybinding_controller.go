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

package agentregistry

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/agentregistry/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/agentregistry/apiv1"
	agentregistrypb "cloud.google.com/go/agentregistry/apiv1/agentregistrypb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.AgentRegistryBindingGVK, NewBindingModel)
}

func NewBindingModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelBinding{config: *config}, nil
}

var _ directbase.Model = &modelBinding{}
var _ = strings.TrimPrefix

type modelBinding struct {
	config config.ControllerConfig
}

func (m *modelBinding) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.AgentRegistryBinding{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.AgentRegistryBindingIdentity{}
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &BindingAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelBinding) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.AgentRegistryBindingIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &BindingAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelBinding) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Binding client: %w", err)
	}
	return gcpClient, err
}

type BindingAdapter struct {
	id        *krm.AgentRegistryBindingIdentity
	gcpClient *gcp.Client
	desired   *krm.AgentRegistryBinding
	actual    *agentregistrypb.Binding
}

var _ directbase.Adapter = &BindingAdapter{}

func (a *BindingAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Binding", "name", a.id)

	req := &agentregistrypb.GetBindingRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetBinding(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Binding %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *BindingAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Binding", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := AgentRegistryBindingSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &agentregistrypb.CreateBindingRequest{
		Parent:    a.id.Parent().String(),
		BindingId: a.id.ID(),
		Binding:   resource,
	}
	op, err := a.gcpClient.CreateBinding(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Binding %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create Binding %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Binding", "name", a.id)

	status := &krm.AgentRegistryBindingStatus{}
	status.ObservedState = AgentRegistryBindingObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *BindingAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Binding", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := AgentRegistryBindingSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareBinding(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &agentregistrypb.UpdateBindingRequest{
			Binding:    desired,
			UpdateMask: updateMask,
		}
		op, err := a.gcpClient.UpdateBinding(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Binding %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update Binding %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Binding", "name", a.id)

		status := &krm.AgentRegistryBindingStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = AgentRegistryBindingObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareBinding(ctx context.Context, actual, desired *agentregistrypb.Binding) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, AgentRegistryBindingSpec_FromProto, AgentRegistryBindingSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *BindingAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.AgentRegistryBinding{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(AgentRegistryBindingSpec_FromProto(mapCtx, a.actual))
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
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.AgentRegistryBindingGVK)
	return u, nil
}

func (a *BindingAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Binding", "name", a.id)

	req := &agentregistrypb.DeleteBindingRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteBinding(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Binding %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Binding", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete Binding %s: %w", a.id, err)
		}
	}
	return true, nil
}
