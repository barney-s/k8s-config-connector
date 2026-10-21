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

package oslogin

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/oslogin/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/oslogin/apiv1"
	commonpb "cloud.google.com/go/oslogin/common/commonpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.OSLoginSSHPublicKeyGVK, NewSshPublicKeyModel)
}

func NewSshPublicKeyModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelSshPublicKey{config: *config}, nil
}

var _ directbase.Model = &modelSshPublicKey{}
var _ = strings.TrimPrefix

type modelSshPublicKey struct {
	config config.ControllerConfig
}

func (m *modelSshPublicKey) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.OSLoginSSHPublicKey{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.OSLoginSSHPublicKeyIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &SshPublicKeyAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelSshPublicKey) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.OSLoginSSHPublicKeyIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &SshPublicKeyAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelSshPublicKey) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building SshPublicKey client: %w", err)
	}
	return gcpClient, err
}

type SshPublicKeyAdapter struct {
	id        *krm.OSLoginSSHPublicKeyIdentity
	gcpClient *gcp.Client
	desired   *krm.OSLoginSSHPublicKey
	actual    *commonpb.SshPublicKey
}

var _ directbase.Adapter = &SshPublicKeyAdapter{}

func (a *SshPublicKeyAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting SshPublicKey", "name", a.id)

	req := &commonpb.GetSshPublicKeyRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetSshPublicKey(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting SshPublicKey %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *SshPublicKeyAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating SshPublicKey", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := OSLoginSSHPublicKeySpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &commonpb.CreateSshPublicKeyRequest{
		Parent:         a.id.ParentString(),
		SshPublicKeyId: a.id.Fingerprint,
		SshPublicKey:   resource,
	}
	created, err := a.gcpClient.CreateSshPublicKey(ctx, req)
	if err != nil {
		return fmt.Errorf("creating SshPublicKey %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created SshPublicKey", "name", a.id)

	status := &krm.OSLoginSSHPublicKeyStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *SshPublicKeyAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating SshPublicKey", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := OSLoginSSHPublicKeySpec_v1alpha1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareSshPublicKey(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &commonpb.UpdateSshPublicKeyRequest{
			SshPublicKey: desired,
			UpdateMask:   updateMask,
		}
		updated, err := a.gcpClient.UpdateSshPublicKey(ctx, req)
		if err != nil {
			return fmt.Errorf("updating SshPublicKey %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated SshPublicKey", "name", a.id)

		status := &krm.OSLoginSSHPublicKeyStatus{}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareSshPublicKey(ctx context.Context, actual, desired *commonpb.SshPublicKey) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, OSLoginSSHPublicKeySpec_v1alpha1_FromProto, OSLoginSSHPublicKeySpec_v1alpha1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *SshPublicKeyAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.OSLoginSSHPublicKey{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(OSLoginSSHPublicKeySpec_v1alpha1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.Fingerprint)
	u.SetGroupVersionKind(krm.OSLoginSSHPublicKeyGVK)
	return u, nil
}

func (a *SshPublicKeyAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting SshPublicKey", "name", a.id)

	req := &commonpb.DeleteSshPublicKeyRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteSshPublicKey(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting SshPublicKey %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted SshPublicKey", "name", a.id)
	return true, nil
}
