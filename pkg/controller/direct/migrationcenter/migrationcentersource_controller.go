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

package migrationcenter

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/migrationcenter/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/migrationcenter/apiv1"
	pb "cloud.google.com/go/migrationcenter/apiv1/migrationcenterpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.MigrationCenterSourceGVK, NewSourceModel)
}

func NewSourceModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelSource{config: *config}, nil
}

var _ directbase.Model = &modelSource{}
var _ = strings.TrimPrefix

type modelSource struct {
	config config.ControllerConfig
}

func (m *modelSource) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.MigrationCenterSource{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.MigrationCenterSourceIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &SourceAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelSource) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.MigrationCenterSourceIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &SourceAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelSource) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Source client: %w", err)
	}
	return gcpClient, err
}

type SourceAdapter struct {
	id        *krm.MigrationCenterSourceIdentity
	gcpClient *gcp.Client
	desired   *krm.MigrationCenterSource
	actual    *pb.Source
}

var _ directbase.Adapter = &SourceAdapter{}

func (a *SourceAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Source", "name", a.id)

	req := &pb.GetSourceRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetSource(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Source %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *SourceAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Source", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := MigrationCenterSourceSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateSourceRequest{
		Parent:   a.id.ParentString(),
		SourceId: a.id.Source,
		Source:   resource,
	}
	op, err := a.gcpClient.CreateSource(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Source %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create Source %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Source", "name", a.id)

	status := &krm.MigrationCenterSourceStatus{}
	status.ObservedState = MigrationCenterSourceObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *SourceAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Source", "name", a.id)

	desired, diffs, updateMask, err := compareSource(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateSourceRequest{
			Source:     desired,
			UpdateMask: updateMask,
		}
		op, err := a.gcpClient.UpdateSource(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Source %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update Source %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Source", "name", a.id)

		status := &krm.MigrationCenterSourceStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = MigrationCenterSourceObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareSource(ctx context.Context, u *unstructured.Unstructured, actual *pb.Source, desiredSpec *krm.MigrationCenterSourceSpec) (*pb.Source, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		MigrationCenterSourceSpec_FromProto,
		MigrationCenterSourceSpec_ToProto,
		nil,
	)
}

func (a *SourceAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.MigrationCenterSource{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(MigrationCenterSourceSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = &a.id.Location
	obj.Spec.ResourceID = &a.id.Source

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Source)
	u.SetGroupVersionKind(krm.MigrationCenterSourceGVK)
	return u, nil
}

func (a *SourceAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Source", "name", a.id)

	req := &pb.DeleteSourceRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteSource(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Source %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Source", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete Source %s: %w", a.id, err)
		}
	}
	return true, nil
}
