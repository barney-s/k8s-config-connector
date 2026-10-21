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

package documentai

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/documentai/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/documentai/apiv1"
	documentaipb "cloud.google.com/go/documentai/apiv1/documentaipb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.DocumentaiprocessorversionGVK, NewProcessorVersionModel)
}

func NewProcessorVersionModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelProcessorVersion{config: *config}, nil
}

var _ directbase.Model = &modelProcessorVersion{}
var _ = strings.TrimPrefix

type modelProcessorVersion struct {
	config config.ControllerConfig
}

func (m *modelProcessorVersion) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.Documentaiprocessorversion{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krmv1beta1.ProcessorVersionIdentity{}
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ProcessorVersionAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelProcessorVersion) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelProcessorVersion) client(ctx context.Context) (*gcp.DocumentProcessorClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewDocumentProcessorRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ProcessorVersion client: %w", err)
	}
	return gcpClient, err
}

type ProcessorVersionAdapter struct {
	id        *krmv1beta1.ProcessorVersionIdentity
	gcpClient *gcp.DocumentProcessorClient
	desired   *krm.Documentaiprocessorversion
	actual    *documentaipb.ProcessorVersion
}

var _ directbase.Adapter = &ProcessorVersionAdapter{}

func (a *ProcessorVersionAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ProcessorVersion", "name", a.id)

	req := &documentaipb.GetProcessorVersionRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetProcessorVersion(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ProcessorVersion %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ProcessorVersionAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ProcessorVersion", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := DocumentaiprocessorversionSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &documentaipb.CreateProcessorVersionRequest{
		Parent:             a.id.Parent().String(),
		ProcessorVersionId: a.id.ID(),
		ProcessorVersion:   resource,
	}
	op, err := a.gcpClient.CreateProcessorVersion(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ProcessorVersion %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create ProcessorVersion %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ProcessorVersion", "name", a.id)

	status := &krm.DocumentaiprocessorversionStatus{}
	status.ObservedState = DocumentaiprocessorversionObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ProcessorVersionAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ProcessorVersion", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := DocumentaiprocessorversionSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareProcessorVersion(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("Documentaiprocessorversion resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.DocumentaiprocessorversionStatus{}
	mapCtx = &direct.MapContext{}
	status.ObservedState = DocumentaiprocessorversionObservedState_FromProto(mapCtx, a.actual)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareProcessorVersion(ctx context.Context, actual, desired *documentaipb.ProcessorVersion) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, DocumentaiprocessorversionSpec_FromProto, DocumentaiprocessorversionSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *ProcessorVersionAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.Documentaiprocessorversion{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(DocumentaiprocessorversionSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.DocumentaiprocessorversionGVK)
	return u, nil
}

func (a *ProcessorVersionAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting ProcessorVersion", "name", a.id)

	req := &documentaipb.DeleteProcessorVersionRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteProcessorVersion(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting ProcessorVersion %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted ProcessorVersion", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete ProcessorVersion %s: %w", a.id, err)
		}
	}
	return true, nil
}
