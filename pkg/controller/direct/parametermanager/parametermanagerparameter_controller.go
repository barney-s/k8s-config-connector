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

package parametermanager

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/parametermanager/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/export"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/label"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/parametermanager/apiv1"
	parametermanagerpb "cloud.google.com/go/parametermanager/apiv1/parametermanagerpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.ParameterManagerParameterGVK, NewParameterModel)
}

func NewParameterModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelParameter{config: *config}, nil
}

var _ directbase.Model = &modelParameter{}
var _ = strings.TrimPrefix

type modelParameter struct {
	config config.ControllerConfig
}

func (m *modelParameter) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.ParameterManagerParameter{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.ParameterIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ParameterAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelParameter) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.ParameterIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ParameterAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelParameter) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Parameter client: %w", err)
	}
	return gcpClient, err
}

type ParameterAdapter struct {
	id        *krm.ParameterIdentity
	gcpClient *gcp.Client
	desired   *krm.ParameterManagerParameter
	actual    *parametermanagerpb.Parameter
}

var _ directbase.Adapter = &ParameterAdapter{}

func (a *ParameterAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Parameter", "name", a.id)

	req := &parametermanagerpb.GetParameterRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetParameter(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Parameter %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ParameterAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Parameter", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := ParameterManagerParameterSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &parametermanagerpb.CreateParameterRequest{
		Parent:      a.id.Parent().String(),
		ParameterId: a.id.ID(),
		Parameter:   resource,
	}
	created, err := a.gcpClient.CreateParameter(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Parameter %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Parameter", "name", a.id)

	status := &krm.ParameterManagerParameterStatus{}
	status.ObservedState = ParameterManagerParameterObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ParameterAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Parameter", "name", a.id)

	desired, diffs, updateMask, err := compareParameter(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &parametermanagerpb.UpdateParameterRequest{
			Parameter:  desired,
			UpdateMask: updateMask,
		}
		updated, err := a.gcpClient.UpdateParameter(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Parameter %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Parameter", "name", a.id)

		status := &krm.ParameterManagerParameterStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = ParameterManagerParameterObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareParameter(ctx context.Context, u *unstructured.Unstructured, actual *parametermanagerpb.Parameter, desiredSpec *krm.ParameterManagerParameterSpec) (*parametermanagerpb.Parameter, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	return common.CompareGreenfieldSpecAndLabels(
		ctx,
		u,
		desiredSpec,
		actual,
		"labels",
		ParameterManagerParameterSpec_FromProto,
		ParameterManagerParameterSpec_ToProto,
		nil,
	)
}

func (a *ParameterAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.ParameterManagerParameter{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(ParameterManagerParameterSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ResourceID = direct.LazyPtr(a.id.ID())

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.ParameterManagerParameterGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *ParameterAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Parameter", "name", a.id)

	req := &parametermanagerpb.DeleteParameterRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteParameter(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Parameter %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Parameter", "name", a.id)
	return true, nil
}
