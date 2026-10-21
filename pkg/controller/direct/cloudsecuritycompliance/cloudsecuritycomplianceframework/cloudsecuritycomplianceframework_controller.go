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

package cloudsecuritycomplianceframework

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/cloudsecuritycompliance/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/cloudsecuritycompliance/apiv1"
	pb "cloud.google.com/go/cloudsecuritycompliance/apiv1/cloudsecuritycompliancepb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.CloudSecurityComplianceFrameworkGVK, NewFrameworkModel)
}

func NewFrameworkModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelFramework{config: *config}, nil
}

var _ directbase.Model = &modelFramework{}
var _ = strings.TrimPrefix

type modelFramework struct {
	config config.ControllerConfig
}

func (m *modelFramework) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.CloudSecurityComplianceFramework{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.CloudSecurityComplianceFrameworkIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &FrameworkAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelFramework) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelFramework) client(ctx context.Context) (*gcp.ConfigClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewConfigRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Framework client: %w", err)
	}
	return gcpClient, err
}

type FrameworkAdapter struct {
	id        *krm.CloudSecurityComplianceFrameworkIdentity
	gcpClient *gcp.ConfigClient
	desired   *krm.CloudSecurityComplianceFramework
	actual    *pb.Framework
}

var _ directbase.Adapter = &FrameworkAdapter{}

func (a *FrameworkAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Framework", "name", a.id)

	req := &pb.GetFrameworkRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetFramework(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Framework %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *FrameworkAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Framework", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := CloudSecurityComplianceFrameworkSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateFrameworkRequest{
		Parent:      a.id.ParentString(),
		FrameworkId: a.id.Framework,
		Framework:   resource,
	}
	created, err := a.gcpClient.CreateFramework(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Framework %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Framework", "name", a.id)

	status := &krm.CloudSecurityComplianceFrameworkStatus{}
	status.ObservedState = CloudSecurityComplianceFrameworkObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *FrameworkAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Framework", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := CloudSecurityComplianceFrameworkSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareFramework(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateFrameworkRequest{
			Framework:  desired,
			UpdateMask: updateMask,
		}
		updated, err := a.gcpClient.UpdateFramework(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Framework %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Framework", "name", a.id)

		status := &krm.CloudSecurityComplianceFrameworkStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = CloudSecurityComplianceFrameworkObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareFramework(ctx context.Context, actual, desired *pb.Framework) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, CloudSecurityComplianceFrameworkSpec_FromProto, CloudSecurityComplianceFrameworkSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *FrameworkAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.CloudSecurityComplianceFramework{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(CloudSecurityComplianceFrameworkSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Framework)
	u.SetGroupVersionKind(krm.CloudSecurityComplianceFrameworkGVK)
	return u, nil
}

func (a *FrameworkAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Framework", "name", a.id)

	req := &pb.DeleteFrameworkRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteFramework(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Framework %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Framework", "name", a.id)
	return true, nil
}
