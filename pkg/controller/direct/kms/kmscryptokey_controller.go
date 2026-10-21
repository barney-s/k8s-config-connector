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

package kms

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/kms/v1beta1"
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

	gcp "cloud.google.com/go/kms/apiv1"
	kmspb "cloud.google.com/go/kms/apiv1/kmspb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.KMSCryptoKeyGVK, NewCryptoKeyModel)
}

func NewCryptoKeyModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelCryptoKey{config: *config}, nil
}

var _ directbase.Model = &modelCryptoKey{}
var _ = strings.TrimPrefix

type modelCryptoKey struct {
	config config.ControllerConfig
}

func (m *modelCryptoKey) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.KMSCryptoKey{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.KMSCryptoKeyIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &CryptoKeyAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelCryptoKey) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.KMSCryptoKeyIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &CryptoKeyAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelCryptoKey) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building CryptoKey client: %w", err)
	}
	return gcpClient, err
}

type CryptoKeyAdapter struct {
	id        *krm.KMSCryptoKeyIdentity
	gcpClient *gcp.Client
	desired   *krm.KMSCryptoKey
	actual    *kmspb.CryptoKey
}

var _ directbase.Adapter = &CryptoKeyAdapter{}

func (a *CryptoKeyAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting CryptoKey", "name", a.id)

	req := &kmspb.GetCryptoKeyRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetCryptoKey(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting CryptoKey %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *CryptoKeyAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating CryptoKey", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := KMSCryptoKeySpec_v1beta1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &kmspb.CreateCryptoKeyRequest{
		Parent:      a.id.ParentString(),
		CryptoKeyId: a.id.CryptoKey,
		CryptoKey:   resource,
	}
	created, err := a.gcpClient.CreateCryptoKey(ctx, req)
	if err != nil {
		return fmt.Errorf("creating CryptoKey %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created CryptoKey", "name", a.id)

	status := &krm.KMSCryptoKeyStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *CryptoKeyAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating CryptoKey", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := KMSCryptoKeySpec_v1beta1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareCryptoKey(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &kmspb.UpdateCryptoKeyRequest{
			CryptoKey:  desired,
			UpdateMask: updateMask,
		}
		updated, err := a.gcpClient.UpdateCryptoKey(ctx, req)
		if err != nil {
			return fmt.Errorf("updating CryptoKey %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated CryptoKey", "name", a.id)

		status := &krm.KMSCryptoKeyStatus{}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareCryptoKey(ctx context.Context, actual, desired *kmspb.CryptoKey) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, KMSCryptoKeySpec_v1beta1_FromProto, KMSCryptoKeySpec_v1beta1_ToProto)
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

func (a *CryptoKeyAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.KMSCryptoKey{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(KMSCryptoKeySpec_v1beta1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ResourceID = direct.LazyPtr(a.id.CryptoKey)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.CryptoKey)
	u.SetGroupVersionKind(krm.KMSCryptoKeyGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *CryptoKeyAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deletion not supported for CryptoKey, removing finalizer without deleting GCP resource", "name", a.id)
	return true, nil
}
