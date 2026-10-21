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

package ccinsightsanalysisrule

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/contactcenterinsights/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/contactcenterinsights/apiv1"
	pb "cloud.google.com/go/contactcenterinsights/apiv1/contactcenterinsightspb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.CCInsightsAnalysisRuleGVK, NewAnalysisRuleModel)
}

func NewAnalysisRuleModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelAnalysisRule{config: *config}, nil
}

var _ directbase.Model = &modelAnalysisRule{}
var _ = strings.TrimPrefix

type modelAnalysisRule struct {
	config config.ControllerConfig
}

func (m *modelAnalysisRule) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.CCInsightsAnalysisRule{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.CCInsightsAnalysisRuleIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &AnalysisRuleAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelAnalysisRule) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.CCInsightsAnalysisRuleIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &AnalysisRuleAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelAnalysisRule) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building AnalysisRule client: %w", err)
	}
	return gcpClient, err
}

type AnalysisRuleAdapter struct {
	id        *krm.CCInsightsAnalysisRuleIdentity
	gcpClient *gcp.Client
	desired   *krm.CCInsightsAnalysisRule
	actual    *pb.AnalysisRule
}

var _ directbase.Adapter = &AnalysisRuleAdapter{}

func (a *AnalysisRuleAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting AnalysisRule", "name", a.id)

	req := &pb.GetAnalysisRuleRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetAnalysisRule(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting AnalysisRule %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *AnalysisRuleAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating AnalysisRule", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := CCInsightsAnalysisRuleSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateAnalysisRuleRequest{
		Parent:         a.id.ParentString(),
		AnalysisRuleId: a.id.FromExternal,
		AnalysisRule:   resource,
	}
	created, err := a.gcpClient.CreateAnalysisRule(ctx, req)
	if err != nil {
		return fmt.Errorf("creating AnalysisRule %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created AnalysisRule", "name", a.id)

	status := &krm.CCInsightsAnalysisRuleStatus{}
	status.ObservedState = CCInsightsAnalysisRuleObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *AnalysisRuleAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating AnalysisRule", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := CCInsightsAnalysisRuleSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareAnalysisRule(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateAnalysisRuleRequest{
			AnalysisRule: desired,
			UpdateMask:   updateMask,
		}
		updated, err := a.gcpClient.UpdateAnalysisRule(ctx, req)
		if err != nil {
			return fmt.Errorf("updating AnalysisRule %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated AnalysisRule", "name", a.id)

		status := &krm.CCInsightsAnalysisRuleStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = CCInsightsAnalysisRuleObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareAnalysisRule(ctx context.Context, actual, desired *pb.AnalysisRule) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, CCInsightsAnalysisRuleSpec_FromProto, CCInsightsAnalysisRuleSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *AnalysisRuleAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.CCInsightsAnalysisRule{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(CCInsightsAnalysisRuleSpec_FromProto(mapCtx, a.actual))
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
	u.SetName(a.id.FromExternal)
	u.SetGroupVersionKind(krm.CCInsightsAnalysisRuleGVK)
	return u, nil
}

func (a *AnalysisRuleAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting AnalysisRule", "name", a.id)

	req := &pb.DeleteAnalysisRuleRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteAnalysisRule(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting AnalysisRule %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted AnalysisRule", "name", a.id)
	return true, nil
}
