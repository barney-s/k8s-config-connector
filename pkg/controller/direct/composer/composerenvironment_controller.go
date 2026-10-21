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

package composer

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/composer/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/export"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/label"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/orchestration/airflow/service/apiv1"
	composerpb "cloud.google.com/go/orchestration/airflow/service/apiv1/servicepb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.ComposerEnvironmentGVK, NewEnvironmentModel)
}

func NewEnvironmentModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelEnvironment{config: *config}, nil
}

var _ directbase.Model = &modelEnvironment{}
var _ = strings.TrimPrefix

type modelEnvironment struct {
	config config.ControllerConfig
}

func (m *modelEnvironment) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.ComposerEnvironment{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.EnvironmentIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &EnvironmentAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelEnvironment) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelEnvironment) client(ctx context.Context) (*gcp.EnvironmentsClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewEnvironmentsRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Environment client: %w", err)
	}
	return gcpClient, err
}

type EnvironmentAdapter struct {
	id        *krm.EnvironmentIdentity
	gcpClient *gcp.EnvironmentsClient
	desired   *krm.ComposerEnvironment
	actual    *composerpb.Environment
}

var _ directbase.Adapter = &EnvironmentAdapter{}

func (a *EnvironmentAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Environment", "name", a.id)

	req := &composerpb.GetEnvironmentRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetEnvironment(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Environment %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *EnvironmentAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Environment", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := ComposerEnvironmentSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &composerpb.CreateEnvironmentRequest{
		Parent:        a.id.Parent().String(),
		EnvironmentId: a.id.Environment,
		Environment:   resource,
	}
	op, err := a.gcpClient.CreateEnvironment(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Environment %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create Environment %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Environment", "name", a.id)

	status := &krm.ComposerEnvironmentStatus{}
	status.ObservedState = ComposerEnvironmentObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *EnvironmentAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Environment", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := ComposerEnvironmentSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareEnvironment(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &composerpb.UpdateEnvironmentRequest{
			Environment: desired,
			UpdateMask:  updateMask,
		}
		op, err := a.gcpClient.UpdateEnvironment(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Environment %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update Environment %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Environment", "name", a.id)

		status := &krm.ComposerEnvironmentStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = ComposerEnvironmentObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareEnvironment(ctx context.Context, actual, desired *composerpb.Environment) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, ComposerEnvironmentSpec_FromProto, ComposerEnvironmentSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	maskedActual.Labels = actual.Labels
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *EnvironmentAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.ComposerEnvironment{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(ComposerEnvironmentSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Environment)
	u.SetGroupVersionKind(krm.ComposerEnvironmentGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *EnvironmentAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Environment", "name", a.id)

	req := &composerpb.DeleteEnvironmentRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteEnvironment(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Environment %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Environment", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete Environment %s: %w", a.id, err)
		}
	}
	return true, nil
}
