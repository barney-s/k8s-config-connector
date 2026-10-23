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

package secretmanager

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/secretmanager/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/export"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/label"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/secretmanager/apiv1"
	secretmanagerpb "cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.SecretManagerSecretGVK, NewSecretModel)
}

func NewSecretModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelSecret{config: *config}, nil
}

var _ directbase.Model = &modelSecret{}
var _ = strings.TrimPrefix

type modelSecret struct {
	config config.ControllerConfig
}

func (m *modelSecret) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.SecretManagerSecret{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id, err := krm.NewSecretIdentity(ctx, reader, obj, u)
	if err != nil {
		return nil, err
	}
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &SecretAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelSecret) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelSecret) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Secret client: %w", err)
	}
	return gcpClient, err
}

type SecretAdapter struct {
	id        *krm.SecretIdentity
	gcpClient *gcp.Client
	desired   *krm.SecretManagerSecret
	actual    *secretmanagerpb.Secret
}

var _ directbase.Adapter = &SecretAdapter{}

func (a *SecretAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Secret", "name", a.id)

	req := &secretmanagerpb.GetSecretRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetSecret(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Secret %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *SecretAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Secret", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := SecretManagerSecretSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &secretmanagerpb.CreateSecretRequest{
		Parent:   a.id.Parent().String(),
		SecretId: a.id.ID(),
		Secret:   resource,
	}
	created, err := a.gcpClient.CreateSecret(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Secret %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Secret", "name", a.id)

	status := &krm.SecretManagerSecretStatus{}
	status.ObservedState = SecretManagerSecretObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *SecretAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Secret", "name", a.id)

	desired, diffs, updateMask, err := compareSecret(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &secretmanagerpb.UpdateSecretRequest{
			Secret:     desired,
			UpdateMask: updateMask,
		}
		updated, err := a.gcpClient.UpdateSecret(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Secret %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Secret", "name", a.id)

		status := &krm.SecretManagerSecretStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = SecretManagerSecretObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareSecret(ctx context.Context, u *unstructured.Unstructured, actual *secretmanagerpb.Secret, desiredSpec *krm.SecretManagerSecretSpec) (*secretmanagerpb.Secret, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	return common.CompareGreenfieldSpecAndLabels(
		ctx,
		u,
		desiredSpec,
		actual,
		"labels",
		SecretManagerSecretSpec_FromProto,
		SecretManagerSecretSpec_ToProto,
		nil,
	)
}

func (a *SecretAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.SecretManagerSecret{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(SecretManagerSecretSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.SecretManagerSecretGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *SecretAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Secret", "name", a.id)

	req := &secretmanagerpb.DeleteSecretRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteSecret(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Secret %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Secret", "name", a.id)
	return true, nil
}
