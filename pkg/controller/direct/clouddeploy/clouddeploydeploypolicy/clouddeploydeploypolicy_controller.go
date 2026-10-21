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

package clouddeploydeploypolicy

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/clouddeploy/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
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

	gcp "cloud.google.com/go/deploy/apiv1"
	clouddeploypb "cloud.google.com/go/deploy/apiv1/deploypb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.DeployDeployPolicyGVK, NewDeployPolicyModel)
}

func NewDeployPolicyModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelDeployPolicy{config: *config}, nil
}

var _ directbase.Model = &modelDeployPolicy{}
var _ = strings.TrimPrefix

type modelDeployPolicy struct {
	config config.ControllerConfig
}

func (m *modelDeployPolicy) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.DeployDeployPolicy{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.DeployPolicyIdentity)

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &DeployPolicyAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelDeployPolicy) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelDeployPolicy) client(ctx context.Context) (*gcp.CloudDeployClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewCloudDeployRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building DeployPolicy client: %w", err)
	}
	return gcpClient, err
}

type DeployPolicyAdapter struct {
	id        *krm.DeployPolicyIdentity
	gcpClient *gcp.CloudDeployClient
	desired   *krm.DeployDeployPolicy
	actual    *clouddeploypb.DeployPolicy
}

var _ directbase.Adapter = &DeployPolicyAdapter{}

func (a *DeployPolicyAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting DeployPolicy", "name", a.id)

	req := &clouddeploypb.GetDeployPolicyRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetDeployPolicy(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting DeployPolicy %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *DeployPolicyAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating DeployPolicy", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := DeployDeployPolicySpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &clouddeploypb.CreateDeployPolicyRequest{
		Parent:         a.id.ParentString(),
		DeployPolicyId: a.id.DeployPolicy,
		DeployPolicy:   resource,
	}
	op, err := a.gcpClient.CreateDeployPolicy(ctx, req)
	if err != nil {
		return fmt.Errorf("creating DeployPolicy %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create DeployPolicy %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created DeployPolicy", "name", a.id)

	status := &krm.DeployDeployPolicyStatus{}
	status.ObservedState = DeployDeployPolicyObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *DeployPolicyAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating DeployPolicy", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := DeployDeployPolicySpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareDeployPolicy(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &clouddeploypb.UpdateDeployPolicyRequest{
			DeployPolicy: desired,
			UpdateMask:   updateMask,
		}
		op, err := a.gcpClient.UpdateDeployPolicy(ctx, req)
		if err != nil {
			return fmt.Errorf("updating DeployPolicy %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update DeployPolicy %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated DeployPolicy", "name", a.id)

		status := &krm.DeployDeployPolicyStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = DeployDeployPolicyObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareDeployPolicy(ctx context.Context, actual, desired *clouddeploypb.DeployPolicy) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, DeployDeployPolicySpec_FromProto, DeployDeployPolicySpec_ToProto)
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

func (a *DeployPolicyAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.DeployDeployPolicy{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(DeployDeployPolicySpec_FromProto(mapCtx, a.actual))
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
	u.SetName(a.id.DeployPolicy)
	u.SetGroupVersionKind(krm.DeployDeployPolicyGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *DeployPolicyAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting DeployPolicy", "name", a.id)

	req := &clouddeploypb.DeleteDeployPolicyRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteDeployPolicy(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting DeployPolicy %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted DeployPolicy", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete DeployPolicy %s: %w", a.id, err)
		}
	}
	return true, nil
}
