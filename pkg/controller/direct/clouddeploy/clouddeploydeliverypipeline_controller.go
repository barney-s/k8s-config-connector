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

package clouddeploy

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/clouddeploy/v1beta1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/deploy/apiv1"
	clouddeploypb "cloud.google.com/go/deploy/apiv1/deploypb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.ClouddeploydeliverypipelineGVK, NewDeliveryPipelineModel)
}

func NewDeliveryPipelineModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelDeliveryPipeline{config: *config}, nil
}

var _ directbase.Model = &modelDeliveryPipeline{}
var _ = strings.TrimPrefix

type modelDeliveryPipeline struct {
	config config.ControllerConfig
}

func (m *modelDeliveryPipeline) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.Clouddeploydeliverypipeline{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krmv1beta1.DeliveryPipelineIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &DeliveryPipelineAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelDeliveryPipeline) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelDeliveryPipeline) client(ctx context.Context) (*gcp.CloudDeployClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewCloudDeployRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building DeliveryPipeline client: %w", err)
	}
	return gcpClient, err
}

type DeliveryPipelineAdapter struct {
	id        *krmv1beta1.DeliveryPipelineIdentity
	gcpClient *gcp.CloudDeployClient
	desired   *krm.Clouddeploydeliverypipeline
	actual    *clouddeploypb.DeliveryPipeline
}

var _ directbase.Adapter = &DeliveryPipelineAdapter{}

func (a *DeliveryPipelineAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting DeliveryPipeline", "name", a.id)

	req := &clouddeploypb.GetDeliveryPipelineRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetDeliveryPipeline(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting DeliveryPipeline %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *DeliveryPipelineAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating DeliveryPipeline", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := ClouddeploydeliverypipelineSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &clouddeploypb.CreateDeliveryPipelineRequest{
		Parent:             a.id.Parent().String(),
		DeliveryPipelineId: a.id.ID(),
		DeliveryPipeline:   resource,
	}
	op, err := a.gcpClient.CreateDeliveryPipeline(ctx, req)
	if err != nil {
		return fmt.Errorf("creating DeliveryPipeline %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create DeliveryPipeline %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created DeliveryPipeline", "name", a.id)

	status := &krm.ClouddeploydeliverypipelineStatus{}
	status.ObservedState = ClouddeploydeliverypipelineObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *DeliveryPipelineAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating DeliveryPipeline", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := ClouddeploydeliverypipelineSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareDeliveryPipeline(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &clouddeploypb.UpdateDeliveryPipelineRequest{
			DeliveryPipeline: desired,
			UpdateMask:       updateMask,
		}
		op, err := a.gcpClient.UpdateDeliveryPipeline(ctx, req)
		if err != nil {
			return fmt.Errorf("updating DeliveryPipeline %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update DeliveryPipeline %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated DeliveryPipeline", "name", a.id)

		status := &krm.ClouddeploydeliverypipelineStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = ClouddeploydeliverypipelineObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareDeliveryPipeline(ctx context.Context, actual, desired *clouddeploypb.DeliveryPipeline) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, ClouddeploydeliverypipelineSpec_FromProto, ClouddeploydeliverypipelineSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *DeliveryPipelineAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.Clouddeploydeliverypipeline{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(ClouddeploydeliverypipelineSpec_FromProto(mapCtx, a.actual))
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
	u.SetGroupVersionKind(krm.ClouddeploydeliverypipelineGVK)
	return u, nil
}

func (a *DeliveryPipelineAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting DeliveryPipeline", "name", a.id)

	req := &clouddeploypb.DeleteDeliveryPipelineRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteDeliveryPipeline(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting DeliveryPipeline %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted DeliveryPipeline", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete DeliveryPipeline %s: %w", a.id, err)
		}
	}
	return true, nil
}
