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

package netapp

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/netapp/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/netapp/apiv1"
	netapppb "cloud.google.com/go/netapp/apiv1/netapppb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.NetAppBackupPolicyGVK, NewBackupPolicyModel)
}

func NewBackupPolicyModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelBackupPolicy{config: *config}, nil
}

var _ directbase.Model = &modelBackupPolicy{}
var _ = strings.TrimPrefix

type modelBackupPolicy struct {
	config config.ControllerConfig
}

func (m *modelBackupPolicy) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.NetAppBackupPolicy{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id, err := krm.NewBackupPolicyIdentity(ctx, reader, obj)
	if err != nil {
		return nil, err
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &BackupPolicyAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelBackupPolicy) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelBackupPolicy) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building BackupPolicy client: %w", err)
	}
	return gcpClient, err
}

type BackupPolicyAdapter struct {
	id        *krm.BackupPolicyIdentity
	gcpClient *gcp.Client
	desired   *krm.NetAppBackupPolicy
	actual    *netapppb.BackupPolicy
}

var _ directbase.Adapter = &BackupPolicyAdapter{}

func (a *BackupPolicyAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting BackupPolicy", "name", a.id)

	req := &netapppb.GetBackupPolicyRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetBackupPolicy(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting BackupPolicy %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *BackupPolicyAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating BackupPolicy", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := NetAppBackupPolicySpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &netapppb.CreateBackupPolicyRequest{
		Parent:         a.id.Parent().String(),
		BackupPolicyId: a.id.ID(),
		BackupPolicy:   resource,
	}
	op, err := a.gcpClient.CreateBackupPolicy(ctx, req)
	if err != nil {
		return fmt.Errorf("creating BackupPolicy %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create BackupPolicy %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created BackupPolicy", "name", a.id)

	status := &krm.NetAppBackupPolicyStatus{}
	status.ObservedState = NetAppBackupPolicyObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *BackupPolicyAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating BackupPolicy", "name", a.id)

	desired, diffs, updateMask, err := compareBackupPolicy(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &netapppb.UpdateBackupPolicyRequest{
			BackupPolicy: desired,
			UpdateMask:   updateMask,
		}
		op, err := a.gcpClient.UpdateBackupPolicy(ctx, req)
		if err != nil {
			return fmt.Errorf("updating BackupPolicy %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update BackupPolicy %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated BackupPolicy", "name", a.id)

		status := &krm.NetAppBackupPolicyStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = NetAppBackupPolicyObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareBackupPolicy(ctx context.Context, u *unstructured.Unstructured, actual *netapppb.BackupPolicy, desiredSpec *krm.NetAppBackupPolicySpec) (*netapppb.BackupPolicy, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		NetAppBackupPolicySpec_FromProto,
		NetAppBackupPolicySpec_ToProto,
		nil,
	)
}

func (a *BackupPolicyAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.NetAppBackupPolicy{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(NetAppBackupPolicySpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Parent().ProjectID}
	obj.Spec.Location = a.id.Parent().Location

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.NetAppBackupPolicyGVK)
	return u, nil
}

func (a *BackupPolicyAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting BackupPolicy", "name", a.id)

	req := &netapppb.DeleteBackupPolicyRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteBackupPolicy(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting BackupPolicy %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted BackupPolicy", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete BackupPolicy %s: %w", a.id, err)
		}
	}
	return true, nil
}
