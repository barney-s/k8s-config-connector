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
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/certificatemanager/v1alpha1"
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
	pb "cloud.google.com/go/certificatemanager/apiv1/certificatemanagerpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.CertificatemanagercertificateissuanceconfigGVK, NewCertificateIssuanceConfigModel)
}

func NewCertificateIssuanceConfigModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelCertificateIssuanceConfig{config: *config}, nil
}

var _ directbase.Model = &modelCertificateIssuanceConfig{}
var _ = strings.TrimPrefix

type modelCertificateIssuanceConfig struct {
	config config.ControllerConfig
}

func (m *modelCertificateIssuanceConfig) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.Certificatemanagercertificateissuanceconfig{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krmcertificatemanagerv1alpha1.CertificateManagerCertificateIssuanceConfigIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &CertificateIssuanceConfigAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelCertificateIssuanceConfig) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krmcertificatemanagerv1alpha1.CertificateManagerCertificateIssuanceConfigIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &CertificateIssuanceConfigAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelCertificateIssuanceConfig) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building CertificateIssuanceConfig client: %w", err)
	}
	return gcpClient, err
}

type CertificateIssuanceConfigAdapter struct {
	id        *krmcertificatemanagerv1alpha1.CertificateManagerCertificateIssuanceConfigIdentity
	gcpClient *gcp.Client
	desired   *krm.Certificatemanagercertificateissuanceconfig
	actual    *pb.CertificateIssuanceConfig
}

var _ directbase.Adapter = &CertificateIssuanceConfigAdapter{}

func (a *CertificateIssuanceConfigAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting CertificateIssuanceConfig", "name", a.id)

	req := &pb.GetCertificateIssuanceConfigRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetCertificateIssuanceConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting CertificateIssuanceConfig %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *CertificateIssuanceConfigAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating CertificateIssuanceConfig", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := CertificatemanagercertificateissuanceconfigSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateCertificateIssuanceConfigRequest{
		Parent:                      a.id.ParentString(),
		CertificateIssuanceConfigId: a.id.CertificateIssuanceConfig,
		CertificateIssuanceConfig:   resource,
	}
	op, err := a.gcpClient.CreateCertificateIssuanceConfig(ctx, req)
	if err != nil {
		return fmt.Errorf("creating CertificateIssuanceConfig %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create CertificateIssuanceConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created CertificateIssuanceConfig", "name", a.id)

	status := &krm.CertificatemanagercertificateissuanceconfigStatus{}
	status.ObservedState = CertificatemanagercertificateissuanceconfigObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *CertificateIssuanceConfigAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating CertificateIssuanceConfig", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := CertificatemanagercertificateissuanceconfigSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareCertificateIssuanceConfig(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("Certificatemanagercertificateissuanceconfig resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.CertificatemanagercertificateissuanceconfigStatus{}
	mapCtx = &direct.MapContext{}
	status.ObservedState = CertificatemanagercertificateissuanceconfigObservedState_FromProto(mapCtx, a.actual)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func compareCertificateIssuanceConfig(ctx context.Context, actual, desired *pb.CertificateIssuanceConfig) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, CertificatemanagercertificateissuanceconfigSpec_FromProto, CertificatemanagercertificateissuanceconfigSpec_ToProto)
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

func (a *CertificateIssuanceConfigAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.Certificatemanagercertificateissuanceconfig{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(CertificatemanagercertificateissuanceconfigSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = a.id.Location
	obj.Spec.ResourceID = direct.LazyPtr(a.id.CertificateIssuanceConfig)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.CertificateIssuanceConfig)
	u.SetGroupVersionKind(krm.CertificatemanagercertificateissuanceconfigGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *CertificateIssuanceConfigAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting CertificateIssuanceConfig", "name", a.id)

	req := &pb.DeleteCertificateIssuanceConfigRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteCertificateIssuanceConfig(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting CertificateIssuanceConfig %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted CertificateIssuanceConfig", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete CertificateIssuanceConfig %s: %w", a.id, err)
		}
	}
	return true, nil
}
