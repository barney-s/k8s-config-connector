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

package networksecurity

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/networksecurity/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/networksecurity/apiv1"
	pb "cloud.google.com/go/networksecurity/apiv1/networksecuritypb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.NetworkSecurityTLSInspectionPolicyGVK, NewTlsInspectionPolicyModel)
}

func NewTlsInspectionPolicyModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelTlsInspectionPolicy{config: *config}, nil
}

var _ directbase.Model = &modelTlsInspectionPolicy{}
var _ = strings.TrimPrefix

type modelTlsInspectionPolicy struct {
	config config.ControllerConfig
}

func (m *modelTlsInspectionPolicy) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.NetworkSecurityTLSInspectionPolicy{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.NetworkSecurityTLSInspectionPolicyIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &TlsInspectionPolicyAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelTlsInspectionPolicy) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelTlsInspectionPolicy) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building TlsInspectionPolicy client: %w", err)
	}
	return gcpClient, err
}

type TlsInspectionPolicyAdapter struct {
	id        *krm.NetworkSecurityTLSInspectionPolicyIdentity
	gcpClient *gcp.Client
	desired   *krm.NetworkSecurityTLSInspectionPolicy
	actual    *pb.TlsInspectionPolicy
}

var _ directbase.Adapter = &TlsInspectionPolicyAdapter{}

func (a *TlsInspectionPolicyAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting TlsInspectionPolicy", "name", a.id)

	req := &pb.GetTlsInspectionPolicyRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetTlsInspectionPolicy(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting TlsInspectionPolicy %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *TlsInspectionPolicyAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating TlsInspectionPolicy", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := NetworkSecurityTLSInspectionPolicySpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateTlsInspectionPolicyRequest{
		Parent:                a.id.ParentString(),
		TlsInspectionPolicyId: a.id.TlsInspectionPolicy,
		TlsInspectionPolicy:   resource,
	}
	op, err := a.gcpClient.CreateTlsInspectionPolicy(ctx, req)
	if err != nil {
		return fmt.Errorf("creating TlsInspectionPolicy %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create TlsInspectionPolicy %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created TlsInspectionPolicy", "name", a.id)

	status := &krm.NetworkSecurityTLSInspectionPolicyStatus{}
	status.ObservedState = NetworkSecurityTLSInspectionPolicyObservedState_v1alpha1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *TlsInspectionPolicyAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating TlsInspectionPolicy", "name", a.id)

	desired, diffs, updateMask, err := compareTlsInspectionPolicy(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateTlsInspectionPolicyRequest{
			TlsInspectionPolicy: desired,
			UpdateMask:          updateMask,
		}
		op, err := a.gcpClient.UpdateTlsInspectionPolicy(ctx, req)
		if err != nil {
			return fmt.Errorf("updating TlsInspectionPolicy %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update TlsInspectionPolicy %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated TlsInspectionPolicy", "name", a.id)

		status := &krm.NetworkSecurityTLSInspectionPolicyStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = NetworkSecurityTLSInspectionPolicyObservedState_v1alpha1_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareTlsInspectionPolicy(ctx context.Context, u *unstructured.Unstructured, actual *pb.TlsInspectionPolicy, desiredSpec *krm.NetworkSecurityTLSInspectionPolicySpec) (*pb.TlsInspectionPolicy, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		NetworkSecurityTLSInspectionPolicySpec_v1alpha1_FromProto,
		NetworkSecurityTLSInspectionPolicySpec_v1alpha1_ToProto,
		nil,
	)
}

func (a *TlsInspectionPolicyAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.NetworkSecurityTLSInspectionPolicy{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(NetworkSecurityTLSInspectionPolicySpec_v1alpha1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = direct.LazyPtr(a.id.Location)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.TlsInspectionPolicy)
	u.SetGroupVersionKind(krm.NetworkSecurityTLSInspectionPolicyGVK)
	return u, nil
}

func (a *TlsInspectionPolicyAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting TlsInspectionPolicy", "name", a.id)

	req := &pb.DeleteTlsInspectionPolicyRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteTlsInspectionPolicy(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting TlsInspectionPolicy %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted TlsInspectionPolicy", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete TlsInspectionPolicy %s: %w", a.id, err)
		}
	}
	return true, nil
}
