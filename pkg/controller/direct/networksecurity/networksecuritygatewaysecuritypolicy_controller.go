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
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
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
	registry.RegisterModel(krm.NetworkSecurityGatewaySecurityPolicyGVK, NewGatewaySecurityPolicyModel)
}

func NewGatewaySecurityPolicyModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelGatewaySecurityPolicy{config: *config}, nil
}

var _ directbase.Model = &modelGatewaySecurityPolicy{}
var _ = strings.TrimPrefix

type modelGatewaySecurityPolicy struct {
	config config.ControllerConfig
}

func (m *modelGatewaySecurityPolicy) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.NetworkSecurityGatewaySecurityPolicy{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.NetworkSecurityGatewaySecurityPolicyIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &GatewaySecurityPolicyAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelGatewaySecurityPolicy) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelGatewaySecurityPolicy) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.GRPCClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building GatewaySecurityPolicy client: %w", err)
	}
	return gcpClient, err
}

type GatewaySecurityPolicyAdapter struct {
	id        *krm.NetworkSecurityGatewaySecurityPolicyIdentity
	gcpClient *gcp.Client
	desired   *krm.NetworkSecurityGatewaySecurityPolicy
	actual    *pb.GatewaySecurityPolicy
}

var _ directbase.Adapter = &GatewaySecurityPolicyAdapter{}

func (a *GatewaySecurityPolicyAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting GatewaySecurityPolicy", "name", a.id)

	req := &pb.GetGatewaySecurityPolicyRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetGatewaySecurityPolicy(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting GatewaySecurityPolicy %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *GatewaySecurityPolicyAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating GatewaySecurityPolicy", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := NetworkSecurityGatewaySecurityPolicySpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateGatewaySecurityPolicyRequest{
		Parent:                  a.id.ParentString(),
		GatewaySecurityPolicyId: a.id.GatewaySecurityPolicy,
		GatewaySecurityPolicy:   resource,
	}
	op, err := a.gcpClient.CreateGatewaySecurityPolicy(ctx, req)
	if err != nil {
		return fmt.Errorf("creating GatewaySecurityPolicy %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create GatewaySecurityPolicy %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created GatewaySecurityPolicy", "name", a.id)

	status := &krm.NetworkSecurityGatewaySecurityPolicyStatus{}
	status.ObservedState = NetworkSecurityGatewaySecurityPolicyObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *GatewaySecurityPolicyAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating GatewaySecurityPolicy", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := NetworkSecurityGatewaySecurityPolicySpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareGatewaySecurityPolicy(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateGatewaySecurityPolicyRequest{
			GatewaySecurityPolicy: desired,
			UpdateMask:            updateMask,
		}
		op, err := a.gcpClient.UpdateGatewaySecurityPolicy(ctx, req)
		if err != nil {
			return fmt.Errorf("updating GatewaySecurityPolicy %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update GatewaySecurityPolicy %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated GatewaySecurityPolicy", "name", a.id)

		status := &krm.NetworkSecurityGatewaySecurityPolicyStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = NetworkSecurityGatewaySecurityPolicyObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareGatewaySecurityPolicy(ctx context.Context, actual, desired *pb.GatewaySecurityPolicy) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, NetworkSecurityGatewaySecurityPolicySpec_FromProto, NetworkSecurityGatewaySecurityPolicySpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *GatewaySecurityPolicyAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.NetworkSecurityGatewaySecurityPolicy{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(NetworkSecurityGatewaySecurityPolicySpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.GatewaySecurityPolicy)
	u.SetGroupVersionKind(krm.NetworkSecurityGatewaySecurityPolicyGVK)
	return u, nil
}

func (a *GatewaySecurityPolicyAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting GatewaySecurityPolicy", "name", a.id)

	req := &pb.DeleteGatewaySecurityPolicyRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteGatewaySecurityPolicy(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting GatewaySecurityPolicy %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted GatewaySecurityPolicy", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete GatewaySecurityPolicy %s: %w", a.id, err)
		}
	}
	return true, nil
}
