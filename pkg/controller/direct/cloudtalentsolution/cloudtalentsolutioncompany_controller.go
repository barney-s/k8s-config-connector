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

package cloudtalentsolution

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/cloudtalentsolution/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/talent/apiv4"
	pb "cloud.google.com/go/talent/apiv4/talentpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.CloudTalentSolutionCompanyGVK, NewCompanyModel)
}

func NewCompanyModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelCompany{config: *config}, nil
}

var _ directbase.Model = &modelCompany{}
var _ = strings.TrimPrefix

type modelCompany struct {
	config config.ControllerConfig
}

func (m *modelCompany) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.CloudTalentSolutionCompany{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.CloudTalentSolutionCompanyIdentity)

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &CompanyAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelCompany) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelCompany) client(ctx context.Context) (*gcp.CompanyClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewCompanyRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Company client: %w", err)
	}
	return gcpClient, err
}

type CompanyAdapter struct {
	id        *krm.CloudTalentSolutionCompanyIdentity
	gcpClient *gcp.CompanyClient
	desired   *krm.CloudTalentSolutionCompany
	actual    *pb.Company
}

var _ directbase.Adapter = &CompanyAdapter{}

func (a *CompanyAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Company", "name", a.id)
	if a.id.Company == "" {
		return false, nil
	}

	req := &pb.GetCompanyRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetCompany(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Company %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *CompanyAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Company", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := CloudTalentSolutionCompanySpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateCompanyRequest{
		Parent:  a.id.ParentString(),
		Company: resource,
	}
	created, err := a.gcpClient.CreateCompany(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Company %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Company", "name", a.id)

	status := &krm.CloudTalentSolutionCompanyStatus{}
	status.ObservedState = CloudTalentSolutionCompanyObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(created.GetName())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *CompanyAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Company", "name", a.id)

	desired, diffs, updateMask, err := compareCompany(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateCompanyRequest{
			Company:    desired,
			UpdateMask: updateMask,
		}
		updated, err := a.gcpClient.UpdateCompany(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Company %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Company", "name", a.id)

		status := &krm.CloudTalentSolutionCompanyStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = CloudTalentSolutionCompanyObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareCompany(ctx context.Context, u *unstructured.Unstructured, actual *pb.Company, desiredSpec *krm.CloudTalentSolutionCompanySpec) (*pb.Company, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		CloudTalentSolutionCompanySpec_FromProto,
		CloudTalentSolutionCompanySpec_ToProto,
		nil,
	)
}

func (a *CompanyAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.CloudTalentSolutionCompany{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(CloudTalentSolutionCompanySpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Company)
	u.SetGroupVersionKind(krm.CloudTalentSolutionCompanyGVK)
	return u, nil
}

func (a *CompanyAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Company", "name", a.id)

	req := &pb.DeleteCompanyRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteCompany(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Company %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Company", "name", a.id)
	return true, nil
}
