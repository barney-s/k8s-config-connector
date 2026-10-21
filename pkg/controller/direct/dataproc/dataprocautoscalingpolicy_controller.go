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

package dataproc

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/dataproc/v1beta1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/dataproc/v2/apiv1"
	pb "cloud.google.com/go/dataproc/v2/apiv1/dataprocpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.DataprocAutoscalingPolicyGVK, NewAutoscalingPolicyModel)
}

func NewAutoscalingPolicyModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelAutoscalingPolicy{config: *config}, nil
}

var _ directbase.Model = &modelAutoscalingPolicy{}
var _ = strings.TrimPrefix

type modelAutoscalingPolicy struct {
	config config.ControllerConfig
}

func (m *modelAutoscalingPolicy) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.DataprocAutoscalingPolicy{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.DataprocAutoscalingPolicyIdentity)

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &AutoscalingPolicyAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelAutoscalingPolicy) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelAutoscalingPolicy) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building AutoscalingPolicy client: %w", err)
	}
	return gcpClient, err
}

type AutoscalingPolicyAdapter struct {
	id        *krm.DataprocAutoscalingPolicyIdentity
	gcpClient *gcp.Client
	desired   *krm.DataprocAutoscalingPolicy
	actual    *pb.AutoscalingPolicy
}

var _ directbase.Adapter = &AutoscalingPolicyAdapter{}

func (a *AutoscalingPolicyAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting AutoscalingPolicy", "name", a.id)

	req := &pb.GetAutoscalingPolicyRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetAutoscalingPolicy(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting AutoscalingPolicy %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *AutoscalingPolicyAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating AutoscalingPolicy", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := DataprocAutoscalingPolicySpec_v1beta1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateAutoscalingPolicyRequest{
		Parent:              a.id.ParentString(),
		AutoscalingPolicyId: a.id.AutoscalingPolicy,
		AutoscalingPolicy:   resource,
	}
	created, err := a.gcpClient.CreateAutoscalingPolicy(ctx, req)
	if err != nil {
		return fmt.Errorf("creating AutoscalingPolicy %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created AutoscalingPolicy", "name", a.id)

	status := &krm.DataprocAutoscalingPolicyStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *AutoscalingPolicyAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating AutoscalingPolicy", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := DataprocAutoscalingPolicySpec_v1beta1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareAutoscalingPolicy(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateAutoscalingPolicyRequest{
			AutoscalingPolicy: desired,
		}
		updated, err := a.gcpClient.UpdateAutoscalingPolicy(ctx, req)
		if err != nil {
			return fmt.Errorf("updating AutoscalingPolicy %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated AutoscalingPolicy", "name", a.id)

		status := &krm.DataprocAutoscalingPolicyStatus{}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareAutoscalingPolicy(ctx context.Context, actual, desired *pb.AutoscalingPolicy) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, DataprocAutoscalingPolicySpec_v1beta1_FromProto, DataprocAutoscalingPolicySpec_v1beta1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *AutoscalingPolicyAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.DataprocAutoscalingPolicy{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(DataprocAutoscalingPolicySpec_v1beta1_FromProto(mapCtx, a.actual))
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
	u.SetName(a.id.AutoscalingPolicy)
	u.SetGroupVersionKind(krm.DataprocAutoscalingPolicyGVK)
	return u, nil
}

func (a *AutoscalingPolicyAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting AutoscalingPolicy", "name", a.id)

	req := &pb.DeleteAutoscalingPolicyRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteAutoscalingPolicy(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting AutoscalingPolicy %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted AutoscalingPolicy", "name", a.id)
	return true, nil
}
