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

package logging

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/logging/v1beta1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/logging/apiv2"
	loggingpb "cloud.google.com/go/logging/apiv2/loggingpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.LoggingLogExclusionGVK, NewExclusionModel)
}

func NewExclusionModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelExclusion{config: *config}, nil
}

var _ directbase.Model = &modelExclusion{}
var _ = strings.TrimPrefix

type modelExclusion struct {
	config config.ControllerConfig
}

func (m *modelExclusion) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.LoggingLogExclusion{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.LoggingLogExclusionIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ExclusionAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelExclusion) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.LoggingLogExclusionIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ExclusionAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelExclusion) client(ctx context.Context) (*gcp.ConfigClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewConfigRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Exclusion client: %w", err)
	}
	return gcpClient, err
}

type ExclusionAdapter struct {
	id        *krm.LoggingLogExclusionIdentity
	gcpClient *gcp.ConfigClient
	desired   *krm.LoggingLogExclusion
	actual    *loggingpb.Exclusion
}

var _ directbase.Adapter = &ExclusionAdapter{}

func (a *ExclusionAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Exclusion", "name", a.id)

	req := &loggingpb.GetExclusionRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetExclusion(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Exclusion %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ExclusionAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Exclusion", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := LoggingLogExclusionSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &loggingpb.CreateExclusionRequest{
		Parent:      a.id.ParentString(),
		ExclusionId: a.id.ID(),
		Exclusion:   resource,
	}
	created, err := a.gcpClient.CreateExclusion(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Exclusion %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Exclusion", "name", a.id)

	status := &krm.LoggingLogExclusionStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ExclusionAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Exclusion", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := LoggingLogExclusionSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareExclusion(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &loggingpb.UpdateExclusionRequest{
			Exclusion:  desired,
			UpdateMask: updateMask,
		}
		updated, err := a.gcpClient.UpdateExclusion(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Exclusion %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Exclusion", "name", a.id)

		status := &krm.LoggingLogExclusionStatus{}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareExclusion(ctx context.Context, actual, desired *loggingpb.Exclusion) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, LoggingLogExclusionSpec_FromProto, LoggingLogExclusionSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *ExclusionAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.LoggingLogExclusion{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(LoggingLogExclusionSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.ResourceID = direct.LazyPtr(a.id.ID())

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.LoggingLogExclusionGVK)
	return u, nil
}

func (a *ExclusionAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Exclusion", "name", a.id)

	req := &loggingpb.DeleteExclusionRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteExclusion(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Exclusion %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Exclusion", "name", a.id)
	return true, nil
}
