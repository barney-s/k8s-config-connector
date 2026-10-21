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

package privateca

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/privateca/v1beta1"
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

	gcp "cloud.google.com/go/security/privateca/apiv1"
	pb "cloud.google.com/go/security/privateca/apiv1/privatecapb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.PrivateCACertificateAuthorityGVK, NewPrivateCACertificateAuthorityModel)
}

func NewPrivateCACertificateAuthorityModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelPrivateCACertificateAuthority{config: *config}, nil
}

var _ directbase.Model = &modelPrivateCACertificateAuthority{}
var _ = strings.TrimPrefix

type modelPrivateCACertificateAuthority struct {
	config config.ControllerConfig
}

func (m *modelPrivateCACertificateAuthority) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.PrivateCACertificateAuthority{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.PrivateCACertificateAuthorityIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &PrivateCACertificateAuthorityAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelPrivateCACertificateAuthority) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.PrivateCACertificateAuthorityIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &PrivateCACertificateAuthorityAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelPrivateCACertificateAuthority) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building PrivateCACertificateAuthority client: %w", err)
	}
	return gcpClient, err
}

type PrivateCACertificateAuthorityAdapter struct {
	id        *krm.PrivateCACertificateAuthorityIdentity
	gcpClient *gcp.Client
	desired   *krm.PrivateCACertificateAuthority
	actual    *pb.PrivateCACertificateAuthority
}

var _ directbase.Adapter = &PrivateCACertificateAuthorityAdapter{}

func (a *PrivateCACertificateAuthorityAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting PrivateCACertificateAuthority", "name", a.id)

	req := &pb.GetPrivateCACertificateAuthorityRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetPrivateCACertificateAuthority(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting PrivateCACertificateAuthority %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *PrivateCACertificateAuthorityAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating PrivateCACertificateAuthority", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := PrivateCACertificateAuthoritySpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreatePrivateCACertificateAuthorityRequest{
		Parent:                        a.id.ParentString(),
		CertificateAuthorityId:        a.id.CertificateAuthority,
		PrivateCACertificateAuthority: resource,
	}
	op, err := a.gcpClient.CreatePrivateCACertificateAuthority(ctx, req)
	if err != nil {
		return fmt.Errorf("creating PrivateCACertificateAuthority %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create PrivateCACertificateAuthority %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created PrivateCACertificateAuthority", "name", a.id)

	status := &krm.PrivateCACertificateAuthorityStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *PrivateCACertificateAuthorityAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating PrivateCACertificateAuthority", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := PrivateCACertificateAuthoritySpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := comparePrivateCACertificateAuthority(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	_ = updateMask
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)
		return fmt.Errorf("PrivateCACertificateAuthority resource is immutable and cannot be updated. Field(s) changed: %v", diffs.FieldIDs())
	}

	status := &krm.PrivateCACertificateAuthorityStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return updateOp.UpdateStatus(ctx, status, nil)
}

func comparePrivateCACertificateAuthority(ctx context.Context, actual, desired *pb.PrivateCACertificateAuthority) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, PrivateCACertificateAuthoritySpec_FromProto, PrivateCACertificateAuthoritySpec_ToProto)
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

func (a *PrivateCACertificateAuthorityAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.PrivateCACertificateAuthority{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(PrivateCACertificateAuthoritySpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = a.id.Location
	obj.Spec.ResourceID = direct.LazyPtr(a.id.CertificateAuthority)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.CertificateAuthority)
	u.SetGroupVersionKind(krm.PrivateCACertificateAuthorityGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *PrivateCACertificateAuthorityAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deletion not supported for PrivateCACertificateAuthority, removing finalizer without deleting GCP resource", "name", a.id)
	return true, nil
}
