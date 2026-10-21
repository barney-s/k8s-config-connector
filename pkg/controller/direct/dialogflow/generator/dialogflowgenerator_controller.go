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

package generator

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

	gcp "cloud.google.com/go/dialogflow/apiv2"
	pb "cloud.google.com/go/dialogflow/apiv2/dialogflowpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.DialogflowGeneratorGVK, NewGeneratorModel)
}

func NewGeneratorModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelGenerator{config: *config}, nil
}

var _ directbase.Model = &modelGenerator{}
var _ = strings.TrimPrefix

type modelGenerator struct {
	config config.ControllerConfig
}

func (m *modelGenerator) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.DialogflowGenerator{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.DialogflowGeneratorIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &GeneratorAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelGenerator) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.DialogflowGeneratorIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &GeneratorAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelGenerator) client(ctx context.Context) (*gcp.GeneratorsClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewGeneratorsRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Generator client: %w", err)
	}
	return gcpClient, err
}

type GeneratorAdapter struct {
	id        *krm.DialogflowGeneratorIdentity
	gcpClient *gcp.GeneratorsClient
	desired   *krm.DialogflowGenerator
	actual    *pb.Generator
}

var _ directbase.Adapter = &GeneratorAdapter{}

func (a *GeneratorAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Generator", "name", a.id)

	req := &pb.GetGeneratorRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetGenerator(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Generator %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *GeneratorAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Generator", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := DialogflowGeneratorSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateGeneratorRequest{
		Parent:      a.id.ParentString(),
		GeneratorId: a.id.Generator,
		Generator:   resource,
	}
	created, err := a.gcpClient.CreateGenerator(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Generator %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Generator", "name", a.id)

	status := &krm.DialogflowGeneratorStatus{}
	status.ObservedState = DialogflowGeneratorObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *GeneratorAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Generator", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := DialogflowGeneratorSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareGenerator(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateGeneratorRequest{
			Generator:  desired,
			UpdateMask: updateMask,
		}
		updated, err := a.gcpClient.UpdateGenerator(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Generator %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Generator", "name", a.id)

		status := &krm.DialogflowGeneratorStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = DialogflowGeneratorObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareGenerator(ctx context.Context, actual, desired *pb.Generator) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, DialogflowGeneratorSpec_FromProto, DialogflowGeneratorSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *GeneratorAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.DialogflowGenerator{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(DialogflowGeneratorSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = a.id.Location
	obj.Spec.ResourceID = direct.LazyPtr(a.id.Generator)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Generator)
	u.SetGroupVersionKind(krm.DialogflowGeneratorGVK)
	return u, nil
}

func (a *GeneratorAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Generator", "name", a.id)

	req := &pb.DeleteGeneratorRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteGenerator(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Generator %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Generator", "name", a.id)
	return true, nil
}
