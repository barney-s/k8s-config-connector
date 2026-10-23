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

package visionai

import (
	"context"
	"fmt"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/visionai/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/export"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/label"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/visionai/apiv1"
	pb "cloud.google.com/go/visionai/apiv1/visionaipb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.VisionAIApplicationGVK, NewApplicationModel)
}

func NewApplicationModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelApplication{config: *config}, nil
}

var _ directbase.Model = &modelApplication{}
var _ = strings.TrimPrefix

type modelApplication struct {
	config config.ControllerConfig
}

func (m *modelApplication) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.VisionAIApplication{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.VisionAIApplicationIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ApplicationAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelApplication) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.VisionAIApplicationIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ApplicationAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelApplication) client(ctx context.Context) (*gcp.AppPlatformClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewAppPlatformRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Application client: %w", err)
	}
	return gcpClient, err
}

type ApplicationAdapter struct {
	id        *krm.VisionAIApplicationIdentity
	gcpClient *gcp.AppPlatformClient
	desired   *krm.VisionAIApplication
	actual    *pb.Application
}

var _ directbase.Adapter = &ApplicationAdapter{}

func (a *ApplicationAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Application", "name", a.id)

	req := &pb.GetApplicationRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetApplication(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Application %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ApplicationAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Application", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := VisionAIApplicationSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateApplicationRequest{
		Parent:        a.id.ParentString(),
		ApplicationId: a.id.Application,
		Application:   resource,
	}
	op, err := a.gcpClient.CreateApplication(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Application %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create Application %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Application", "name", a.id)

	status := &krm.VisionAIApplicationStatus{}
	status.ObservedState = VisionAIApplicationObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ApplicationAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Application", "name", a.id)

	desired, diffs, updateMask, err := compareApplication(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateApplicationRequest{
			Application: desired,
			UpdateMask:  updateMask,
		}
		op, err := a.gcpClient.UpdateApplication(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Application %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update Application %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Application", "name", a.id)

		status := &krm.VisionAIApplicationStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = VisionAIApplicationObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareApplication(ctx context.Context, u *unstructured.Unstructured, actual *pb.Application, desiredSpec *krm.VisionAIApplicationSpec) (*pb.Application, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	return common.CompareGreenfieldSpecAndLabels(
		ctx,
		u,
		desiredSpec,
		actual,
		"labels",
		VisionAIApplicationSpec_FromProto,
		VisionAIApplicationSpec_ToProto,
		nil,
	)
}

func (a *ApplicationAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.VisionAIApplication{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(VisionAIApplicationSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = direct.LazyPtr(a.id.Location)
	obj.Spec.ResourceID = direct.LazyPtr(a.id.Application)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Application)
	u.SetGroupVersionKind(krm.VisionAIApplicationGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *ApplicationAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Application", "name", a.id)

	req := &pb.DeleteApplicationRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteApplication(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Application %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Application", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete Application %s: %w", a.id, err)
		}
	}
	return true, nil
}
