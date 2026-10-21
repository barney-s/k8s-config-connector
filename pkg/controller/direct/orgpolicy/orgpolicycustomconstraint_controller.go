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

package orgpolicy

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/orgpolicy/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/orgpolicy/apiv2"
	orgpolicypb "cloud.google.com/go/orgpolicy/apiv2/orgpolicypb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.OrgpolicycustomconstraintGVK, NewCustomConstraintModel)
}

func NewCustomConstraintModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelCustomConstraint{config: *config}, nil
}

var _ directbase.Model = &modelCustomConstraint{}
var _ = strings.TrimPrefix

type modelCustomConstraint struct {
	config config.ControllerConfig
}

func (m *modelCustomConstraint) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.Orgpolicycustomconstraint{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krmv1beta1.CustomConstraintIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &CustomConstraintAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelCustomConstraint) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelCustomConstraint) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building CustomConstraint client: %w", err)
	}
	return gcpClient, err
}

type CustomConstraintAdapter struct {
	id        *krmv1beta1.CustomConstraintIdentity
	gcpClient *gcp.Client
	desired   *krm.Orgpolicycustomconstraint
	actual    *orgpolicypb.CustomConstraint
}

var _ directbase.Adapter = &CustomConstraintAdapter{}

func (a *CustomConstraintAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting CustomConstraint", "name", a.id)

	req := &orgpolicypb.GetCustomConstraintRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetCustomConstraint(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting CustomConstraint %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *CustomConstraintAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating CustomConstraint", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := OrgpolicycustomconstraintSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &orgpolicypb.CreateCustomConstraintRequest{
		Parent:             a.id.Parent().String(),
		CustomConstraintId: a.id.CustomConstraint,
		CustomConstraint:   resource,
	}
	created, err := a.gcpClient.CreateCustomConstraint(ctx, req)
	if err != nil {
		return fmt.Errorf("creating CustomConstraint %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created CustomConstraint", "name", a.id)

	status := &krm.OrgpolicycustomconstraintStatus{}
	status.ObservedState = OrgpolicycustomconstraintObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *CustomConstraintAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating CustomConstraint", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := OrgpolicycustomconstraintSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareCustomConstraint(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &orgpolicypb.UpdateCustomConstraintRequest{
			CustomConstraint: desired,
		}
		updated, err := a.gcpClient.UpdateCustomConstraint(ctx, req)
		if err != nil {
			return fmt.Errorf("updating CustomConstraint %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated CustomConstraint", "name", a.id)

		status := &krm.OrgpolicycustomconstraintStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = OrgpolicycustomconstraintObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareCustomConstraint(ctx context.Context, actual, desired *orgpolicypb.CustomConstraint) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, OrgpolicycustomconstraintSpec_FromProto, OrgpolicycustomconstraintSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *CustomConstraintAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.Orgpolicycustomconstraint{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(OrgpolicycustomconstraintSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.CustomConstraint)
	u.SetGroupVersionKind(krm.OrgpolicycustomconstraintGVK)
	return u, nil
}

func (a *CustomConstraintAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting CustomConstraint", "name", a.id)

	req := &orgpolicypb.DeleteCustomConstraintRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteCustomConstraint(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting CustomConstraint %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted CustomConstraint", "name", a.id)
	return true, nil
}
