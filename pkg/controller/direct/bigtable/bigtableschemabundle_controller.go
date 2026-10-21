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

package bigtable

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/bigtable/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/bigtable/admin/apiv2"
	pb "cloud.google.com/go/bigtable/admin/apiv2/adminpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.BigtableSchemaBundleGVK, NewSchemaBundleModel)
}

func NewSchemaBundleModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelSchemaBundle{config: *config}, nil
}

var _ directbase.Model = &modelSchemaBundle{}
var _ = strings.TrimPrefix

type modelSchemaBundle struct {
	config config.ControllerConfig
}

func (m *modelSchemaBundle) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.BigtableSchemaBundle{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.BigtableSchemaBundleIdentity{}
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &SchemaBundleAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelSchemaBundle) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelSchemaBundle) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.GRPCClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building SchemaBundle client: %w", err)
	}
	return gcpClient, err
}

type SchemaBundleAdapter struct {
	id        *krm.BigtableSchemaBundleIdentity
	gcpClient *gcp.Client
	desired   *krm.BigtableSchemaBundle
	actual    *pb.SchemaBundle
}

var _ directbase.Adapter = &SchemaBundleAdapter{}

func (a *SchemaBundleAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting SchemaBundle", "name", a.id)

	req := &pb.GetSchemaBundleRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetSchemaBundle(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting SchemaBundle %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *SchemaBundleAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating SchemaBundle", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := BigtableSchemaBundleSpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateSchemaBundleRequest{
		Parent:         a.id.ParentString(),
		SchemaBundleId: a.id.ID(),
		SchemaBundle:   resource,
	}
	op, err := a.gcpClient.CreateSchemaBundle(ctx, req)
	if err != nil {
		return fmt.Errorf("creating SchemaBundle %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create SchemaBundle %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created SchemaBundle", "name", a.id)

	status := &krm.BigtableSchemaBundleStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *SchemaBundleAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating SchemaBundle", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := BigtableSchemaBundleSpec_v1alpha1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareSchemaBundle(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateSchemaBundleRequest{
			SchemaBundle: desired,
			UpdateMask:   updateMask,
		}
		op, err := a.gcpClient.UpdateSchemaBundle(ctx, req)
		if err != nil {
			return fmt.Errorf("updating SchemaBundle %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update SchemaBundle %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated SchemaBundle", "name", a.id)

		status := &krm.BigtableSchemaBundleStatus{}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareSchemaBundle(ctx context.Context, actual, desired *pb.SchemaBundle) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, BigtableSchemaBundleSpec_v1alpha1_FromProto, BigtableSchemaBundleSpec_v1alpha1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *SchemaBundleAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.BigtableSchemaBundle{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(BigtableSchemaBundleSpec_v1alpha1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.BigtableSchemaBundleGVK)
	return u, nil
}

func (a *SchemaBundleAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting SchemaBundle", "name", a.id)

	req := &pb.DeleteSchemaBundleRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteSchemaBundle(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting SchemaBundle %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted SchemaBundle", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete SchemaBundle %s: %w", a.id, err)
		}
	}
	return true, nil
}
