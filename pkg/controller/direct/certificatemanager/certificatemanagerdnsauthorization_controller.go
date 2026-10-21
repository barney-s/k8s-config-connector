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

package certificatemanager

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/certificatemanager/v1beta1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/export"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/label"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/certificatemanager/apiv1"
	certificatemanagerpb "cloud.google.com/go/certificatemanager/apiv1/certificatemanagerpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.CertificateManagerDNSAuthorizationGVK, NewDnsAuthorizationModel)
}

func NewDnsAuthorizationModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelDnsAuthorization{config: *config}, nil
}

var _ directbase.Model = &modelDnsAuthorization{}
var _ = strings.TrimPrefix

type modelDnsAuthorization struct {
	config config.ControllerConfig
}

func (m *modelDnsAuthorization) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.CertificateManagerDNSAuthorization{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.CertificateManagerDNSAuthorizationIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &DnsAuthorizationAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelDnsAuthorization) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.CertificateManagerDNSAuthorizationIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &DnsAuthorizationAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelDnsAuthorization) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building DnsAuthorization client: %w", err)
	}
	return gcpClient, err
}

type DnsAuthorizationAdapter struct {
	id        *krm.CertificateManagerDNSAuthorizationIdentity
	gcpClient *gcp.Client
	desired   *krm.CertificateManagerDNSAuthorization
	actual    *certificatemanagerpb.DnsAuthorization
}

var _ directbase.Adapter = &DnsAuthorizationAdapter{}

func (a *DnsAuthorizationAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting DnsAuthorization", "name", a.id)

	req := &certificatemanagerpb.GetDnsAuthorizationRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetDnsAuthorization(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting DnsAuthorization %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *DnsAuthorizationAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating DnsAuthorization", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := CertificateManagerDNSAuthorizationSpec_v1beta1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &certificatemanagerpb.CreateDnsAuthorizationRequest{
		Parent:             a.id.ParentString(),
		DnsAuthorizationId: a.id.DNSAuthorization,
		DnsAuthorization:   resource,
	}
	op, err := a.gcpClient.CreateDnsAuthorization(ctx, req)
	if err != nil {
		return fmt.Errorf("creating DnsAuthorization %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create DnsAuthorization %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created DnsAuthorization", "name", a.id)

	status := &krm.CertificateManagerDNSAuthorizationStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *DnsAuthorizationAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating DnsAuthorization", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := CertificateManagerDNSAuthorizationSpec_v1beta1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareDnsAuthorization(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &certificatemanagerpb.UpdateDnsAuthorizationRequest{
			DnsAuthorization: desired,
			UpdateMask:       updateMask,
		}
		op, err := a.gcpClient.UpdateDnsAuthorization(ctx, req)
		if err != nil {
			return fmt.Errorf("updating DnsAuthorization %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update DnsAuthorization %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated DnsAuthorization", "name", a.id)

		status := &krm.CertificateManagerDNSAuthorizationStatus{}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareDnsAuthorization(ctx context.Context, actual, desired *certificatemanagerpb.DnsAuthorization) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, CertificateManagerDNSAuthorizationSpec_v1beta1_FromProto, CertificateManagerDNSAuthorizationSpec_v1beta1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	maskedActual.Labels = actual.Labels
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *DnsAuthorizationAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.CertificateManagerDNSAuthorization{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(CertificateManagerDNSAuthorizationSpec_v1beta1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = a.id.Location
	obj.Spec.ResourceID = direct.LazyPtr(a.id.DNSAuthorization)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.DNSAuthorization)
	u.SetGroupVersionKind(krm.CertificateManagerDNSAuthorizationGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *DnsAuthorizationAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting DnsAuthorization", "name", a.id)

	req := &certificatemanagerpb.DeleteDnsAuthorizationRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteDnsAuthorization(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting DnsAuthorization %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted DnsAuthorization", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete DnsAuthorization %s: %w", a.id, err)
		}
	}
	return true, nil
}
