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

package recaptchaenterprise

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/recaptchaenterprise/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/recaptchaenterprise/v2/apiv1"
	pb "cloud.google.com/go/recaptchaenterprise/v2/apiv1/recaptchaenterprisepb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.ReCAPTCHAEnterpriseFirewallPolicyGVK, NewFirewallPolicyModel)
}

func NewFirewallPolicyModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelFirewallPolicy{config: *config}, nil
}

var _ directbase.Model = &modelFirewallPolicy{}
var _ = strings.TrimPrefix

type modelFirewallPolicy struct {
	config config.ControllerConfig
}

func (m *modelFirewallPolicy) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.ReCAPTCHAEnterpriseFirewallPolicy{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.FirewallPolicyIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &FirewallPolicyAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelFirewallPolicy) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelFirewallPolicy) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building FirewallPolicy client: %w", err)
	}
	return gcpClient, err
}

type FirewallPolicyAdapter struct {
	id        *krm.FirewallPolicyIdentity
	gcpClient *gcp.Client
	desired   *krm.ReCAPTCHAEnterpriseFirewallPolicy
	actual    *pb.FirewallPolicy
}

var _ directbase.Adapter = &FirewallPolicyAdapter{}

func (a *FirewallPolicyAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting FirewallPolicy", "name", a.id)

	req := &pb.GetFirewallPolicyRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetFirewallPolicy(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting FirewallPolicy %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *FirewallPolicyAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating FirewallPolicy", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := ReCAPTCHAEnterpriseFirewallPolicySpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateFirewallPolicyRequest{
		Parent:           a.id.Parent().String(),
		FirewallPolicyId: a.id.ID(),
		FirewallPolicy:   resource,
	}
	created, err := a.gcpClient.CreateFirewallPolicy(ctx, req)
	if err != nil {
		return fmt.Errorf("creating FirewallPolicy %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created FirewallPolicy", "name", a.id)

	status := &krm.ReCAPTCHAEnterpriseFirewallPolicyStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *FirewallPolicyAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating FirewallPolicy", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := ReCAPTCHAEnterpriseFirewallPolicySpec_v1alpha1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareFirewallPolicy(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateFirewallPolicyRequest{
			FirewallPolicy: desired,
			UpdateMask:     updateMask,
		}
		updated, err := a.gcpClient.UpdateFirewallPolicy(ctx, req)
		if err != nil {
			return fmt.Errorf("updating FirewallPolicy %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated FirewallPolicy", "name", a.id)

		status := &krm.ReCAPTCHAEnterpriseFirewallPolicyStatus{}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareFirewallPolicy(ctx context.Context, actual, desired *pb.FirewallPolicy) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, ReCAPTCHAEnterpriseFirewallPolicySpec_v1alpha1_FromProto, ReCAPTCHAEnterpriseFirewallPolicySpec_v1alpha1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *FirewallPolicyAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.ReCAPTCHAEnterpriseFirewallPolicy{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(ReCAPTCHAEnterpriseFirewallPolicySpec_v1alpha1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.ReCAPTCHAEnterpriseFirewallPolicyGVK)
	return u, nil
}

func (a *FirewallPolicyAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting FirewallPolicy", "name", a.id)

	req := &pb.DeleteFirewallPolicyRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteFirewallPolicy(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting FirewallPolicy %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted FirewallPolicy", "name", a.id)
	return true, nil
}
