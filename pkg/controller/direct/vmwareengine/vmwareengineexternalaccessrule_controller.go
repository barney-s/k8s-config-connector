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

package vmwareengine

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/vmwareengine/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/vmwareengine/apiv1"
	pb "cloud.google.com/go/vmwareengine/apiv1/vmwareenginepb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.VMwareEngineExternalAccessRuleGVK, NewExternalAccessRuleModel)
}

func NewExternalAccessRuleModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelExternalAccessRule{config: *config}, nil
}

var _ directbase.Model = &modelExternalAccessRule{}
var _ = strings.TrimPrefix

type modelExternalAccessRule struct {
	config config.ControllerConfig
}

func (m *modelExternalAccessRule) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.VMwareEngineExternalAccessRule{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id, err := krm.NewExternalAccessRuleIdentity(ctx, reader, obj)
	if err != nil {
		return nil, err
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ExternalAccessRuleAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelExternalAccessRule) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelExternalAccessRule) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ExternalAccessRule client: %w", err)
	}
	return gcpClient, err
}

type ExternalAccessRuleAdapter struct {
	id        *krm.ExternalAccessRuleIdentity
	gcpClient *gcp.Client
	desired   *krm.VMwareEngineExternalAccessRule
	actual    *pb.ExternalAccessRule
}

var _ directbase.Adapter = &ExternalAccessRuleAdapter{}

func (a *ExternalAccessRuleAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ExternalAccessRule", "name", a.id)

	req := &pb.GetExternalAccessRuleRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetExternalAccessRule(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ExternalAccessRule %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ExternalAccessRuleAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ExternalAccessRule", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := VMwareEngineExternalAccessRuleSpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateExternalAccessRuleRequest{
		Parent:               a.id.Parent().String(),
		ExternalAccessRuleId: a.id.ID(),
		ExternalAccessRule:   resource,
	}
	op, err := a.gcpClient.CreateExternalAccessRule(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ExternalAccessRule %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create ExternalAccessRule %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ExternalAccessRule", "name", a.id)

	status := &krm.VMwareEngineExternalAccessRuleStatus{}
	status.ObservedState = VMwareEngineExternalAccessRuleObservedState_v1alpha1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ExternalAccessRuleAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ExternalAccessRule", "name", a.id)

	desired, diffs, updateMask, err := compareExternalAccessRule(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateExternalAccessRuleRequest{
			ExternalAccessRule: desired,
			UpdateMask:         updateMask,
		}
		op, err := a.gcpClient.UpdateExternalAccessRule(ctx, req)
		if err != nil {
			return fmt.Errorf("updating ExternalAccessRule %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update ExternalAccessRule %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated ExternalAccessRule", "name", a.id)

		status := &krm.VMwareEngineExternalAccessRuleStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = VMwareEngineExternalAccessRuleObservedState_v1alpha1_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareExternalAccessRule(ctx context.Context, u *unstructured.Unstructured, actual *pb.ExternalAccessRule, desiredSpec *krm.VMwareEngineExternalAccessRuleSpec) (*pb.ExternalAccessRule, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		VMwareEngineExternalAccessRuleSpec_v1alpha1_FromProto,
		VMwareEngineExternalAccessRuleSpec_v1alpha1_ToProto,
		nil,
	)
}

func (a *ExternalAccessRuleAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.VMwareEngineExternalAccessRule{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(VMwareEngineExternalAccessRuleSpec_v1alpha1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.VMwareEngineExternalAccessRuleGVK)
	return u, nil
}

func (a *ExternalAccessRuleAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting ExternalAccessRule", "name", a.id)

	req := &pb.DeleteExternalAccessRuleRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteExternalAccessRule(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting ExternalAccessRule %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted ExternalAccessRule", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete ExternalAccessRule %s: %w", a.id, err)
		}
	}
	return true, nil
}
