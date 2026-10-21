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

package rapidmigrationassessment

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/rapidmigrationassessment/v1alpha1"
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

	gcp "cloud.google.com/go/rapidmigrationassessment/apiv1"
	pb "cloud.google.com/go/rapidmigrationassessment/apiv1/rapidmigrationassessmentpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.RapidMigrationAssessmentCollectorGVK, NewCollectorModel)
}

func NewCollectorModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelCollector{config: *config}, nil
}

var _ directbase.Model = &modelCollector{}
var _ = strings.TrimPrefix

type modelCollector struct {
	config config.ControllerConfig
}

func (m *modelCollector) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.RapidMigrationAssessmentCollector{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.RapidMigrationAssessmentCollectorIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &CollectorAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelCollector) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelCollector) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Collector client: %w", err)
	}
	return gcpClient, err
}

type CollectorAdapter struct {
	id        *krm.RapidMigrationAssessmentCollectorIdentity
	gcpClient *gcp.Client
	desired   *krm.RapidMigrationAssessmentCollector
	actual    *pb.Collector
}

var _ directbase.Adapter = &CollectorAdapter{}

func (a *CollectorAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Collector", "name", a.id)

	req := &pb.GetCollectorRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetCollector(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Collector %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *CollectorAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Collector", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := RapidMigrationAssessmentCollectorSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateCollectorRequest{
		Parent:      a.id.ParentString(),
		CollectorId: a.id.Collector,
		Collector:   resource,
	}
	op, err := a.gcpClient.CreateCollector(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Collector %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create Collector %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Collector", "name", a.id)

	status := &krm.RapidMigrationAssessmentCollectorStatus{}
	status.ObservedState = RapidMigrationAssessmentCollectorObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *CollectorAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Collector", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := RapidMigrationAssessmentCollectorSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareCollector(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateCollectorRequest{
			Collector:  desired,
			UpdateMask: updateMask,
		}
		op, err := a.gcpClient.UpdateCollector(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Collector %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update Collector %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Collector", "name", a.id)

		status := &krm.RapidMigrationAssessmentCollectorStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = RapidMigrationAssessmentCollectorObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareCollector(ctx context.Context, actual, desired *pb.Collector) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, RapidMigrationAssessmentCollectorSpec_FromProto, RapidMigrationAssessmentCollectorSpec_ToProto)
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

func (a *CollectorAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.RapidMigrationAssessmentCollector{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(RapidMigrationAssessmentCollectorSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Collector)
	u.SetGroupVersionKind(krm.RapidMigrationAssessmentCollectorGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *CollectorAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Collector", "name", a.id)

	req := &pb.DeleteCollectorRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteCollector(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Collector %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Collector", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete Collector %s: %w", a.id, err)
		}
	}
	return true, nil
}
