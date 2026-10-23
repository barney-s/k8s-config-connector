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

package configdelivery

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/configdelivery/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/export"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/label"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/configdelivery/apiv1"
	pb "cloud.google.com/go/configdelivery/apiv1/configdeliverypb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.ConfigDeliveryResourceBundleGVK, NewResourceBundleModel)
}

func NewResourceBundleModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelResourceBundle{config: *config}, nil
}

var _ directbase.Model = &modelResourceBundle{}
var _ = strings.TrimPrefix

type modelResourceBundle struct {
	config config.ControllerConfig
}

func (m *modelResourceBundle) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.ConfigDeliveryResourceBundle{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.ConfigDeliveryResourceBundleIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ResourceBundleAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelResourceBundle) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.ConfigDeliveryResourceBundleIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ResourceBundleAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelResourceBundle) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ResourceBundle client: %w", err)
	}
	return gcpClient, err
}

type ResourceBundleAdapter struct {
	id        *krm.ConfigDeliveryResourceBundleIdentity
	gcpClient *gcp.Client
	desired   *krm.ConfigDeliveryResourceBundle
	actual    *pb.ResourceBundle
}

var _ directbase.Adapter = &ResourceBundleAdapter{}

func (a *ResourceBundleAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ResourceBundle", "name", a.id)

	req := &pb.GetResourceBundleRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetResourceBundle(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ResourceBundle %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ResourceBundleAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ResourceBundle", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := ConfigDeliveryResourceBundleSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateResourceBundleRequest{
		Parent:           a.id.ParentString(),
		ResourceBundleId: a.id.ResourceBundle,
		ResourceBundle:   resource,
	}
	op, err := a.gcpClient.CreateResourceBundle(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ResourceBundle %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create ResourceBundle %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ResourceBundle", "name", a.id)

	status := &krm.ConfigDeliveryResourceBundleStatus{}
	status.ObservedState = ConfigDeliveryResourceBundleObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ResourceBundleAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ResourceBundle", "name", a.id)

	desired, diffs, updateMask, err := compareResourceBundle(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateResourceBundleRequest{
			ResourceBundle: desired,
			UpdateMask:     updateMask,
		}
		op, err := a.gcpClient.UpdateResourceBundle(ctx, req)
		if err != nil {
			return fmt.Errorf("updating ResourceBundle %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update ResourceBundle %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated ResourceBundle", "name", a.id)

		status := &krm.ConfigDeliveryResourceBundleStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = ConfigDeliveryResourceBundleObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareResourceBundle(ctx context.Context, u *unstructured.Unstructured, actual *pb.ResourceBundle, desiredSpec *krm.ConfigDeliveryResourceBundleSpec) (*pb.ResourceBundle, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	return common.CompareGreenfieldSpecAndLabels(
		ctx,
		u,
		desiredSpec,
		actual,
		"labels",
		ConfigDeliveryResourceBundleSpec_FromProto,
		ConfigDeliveryResourceBundleSpec_ToProto,
		nil,
	)
}

func (a *ResourceBundleAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.ConfigDeliveryResourceBundle{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(ConfigDeliveryResourceBundleSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ResourceID = direct.LazyPtr(a.id.ResourceBundle)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ResourceBundle)
	u.SetGroupVersionKind(krm.ConfigDeliveryResourceBundleGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *ResourceBundleAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting ResourceBundle", "name", a.id)

	req := &pb.DeleteResourceBundleRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteResourceBundle(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting ResourceBundle %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted ResourceBundle", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete ResourceBundle %s: %w", a.id, err)
		}
	}
	return true, nil
}
