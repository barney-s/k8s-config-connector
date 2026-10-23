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

package saasservicemgmt

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/saasservicemgmt/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/export"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/label"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/saasplatform/saasservicemgmt/apiv1beta1"
	pb "cloud.google.com/go/saasplatform/saasservicemgmt/apiv1beta1/saasservicemgmtpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.SaasServiceMgmtReleaseGVK, NewReleaseModel)
}

func NewReleaseModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelRelease{config: *config}, nil
}

var _ directbase.Model = &modelRelease{}
var _ = strings.TrimPrefix

type modelRelease struct {
	config config.ControllerConfig
}

func (m *modelRelease) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.SaasServiceMgmtRelease{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.SaasServiceMgmtReleaseIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ReleaseAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelRelease) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelRelease) client(ctx context.Context) (*gcp.SaasDeploymentsClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewSaasDeploymentsRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Release client: %w", err)
	}
	return gcpClient, err
}

type ReleaseAdapter struct {
	id        *krm.SaasServiceMgmtReleaseIdentity
	gcpClient *gcp.SaasDeploymentsClient
	desired   *krm.SaasServiceMgmtRelease
	actual    *pb.Release
}

var _ directbase.Adapter = &ReleaseAdapter{}

func (a *ReleaseAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Release", "name", a.id)

	req := &pb.GetReleaseRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetRelease(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Release %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ReleaseAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Release", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := SaasServiceMgmtReleaseSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateReleaseRequest{
		Parent:    a.id.ParentString(),
		ReleaseId: a.id.Release,
		Release:   resource,
	}
	created, err := a.gcpClient.CreateRelease(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Release %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Release", "name", a.id)

	status := &krm.SaasServiceMgmtReleaseStatus{}
	status.ObservedState = SaasServiceMgmtReleaseObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ReleaseAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Release", "name", a.id)

	desired, diffs, updateMask, err := compareRelease(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateReleaseRequest{
			Release:    desired,
			UpdateMask: updateMask,
		}
		updated, err := a.gcpClient.UpdateRelease(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Release %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Release", "name", a.id)

		status := &krm.SaasServiceMgmtReleaseStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = SaasServiceMgmtReleaseObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareRelease(ctx context.Context, u *unstructured.Unstructured, actual *pb.Release, desiredSpec *krm.SaasServiceMgmtReleaseSpec) (*pb.Release, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	return common.CompareGreenfieldSpecAndLabels(
		ctx,
		u,
		desiredSpec,
		actual,
		"labels",
		SaasServiceMgmtReleaseSpec_FromProto,
		SaasServiceMgmtReleaseSpec_ToProto,
		nil,
	)
}

func (a *ReleaseAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.SaasServiceMgmtRelease{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(SaasServiceMgmtReleaseSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Release)
	u.SetGroupVersionKind(krm.SaasServiceMgmtReleaseGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *ReleaseAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Release", "name", a.id)

	req := &pb.DeleteReleaseRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteRelease(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Release %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Release", "name", a.id)
	return true, nil
}
