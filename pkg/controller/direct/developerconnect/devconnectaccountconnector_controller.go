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

package developerconnect

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/developerconnect/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/export"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/label"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/developerconnect/apiv1"
	pb "cloud.google.com/go/developerconnect/apiv1/developerconnectpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.DevConnectAccountConnectorGVK, NewAccountConnectorModel)
}

func NewAccountConnectorModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelAccountConnector{config: *config}, nil
}

var _ directbase.Model = &modelAccountConnector{}
var _ = strings.TrimPrefix

type modelAccountConnector struct {
	config config.ControllerConfig
}

func (m *modelAccountConnector) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.DevConnectAccountConnector{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.DevConnectAccountConnectorIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &AccountConnectorAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelAccountConnector) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.DevConnectAccountConnectorIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &AccountConnectorAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelAccountConnector) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building AccountConnector client: %w", err)
	}
	return gcpClient, err
}

type AccountConnectorAdapter struct {
	id        *krm.DevConnectAccountConnectorIdentity
	gcpClient *gcp.Client
	desired   *krm.DevConnectAccountConnector
	actual    *pb.AccountConnector
}

var _ directbase.Adapter = &AccountConnectorAdapter{}

func (a *AccountConnectorAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting AccountConnector", "name", a.id)

	req := &pb.GetAccountConnectorRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetAccountConnector(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting AccountConnector %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *AccountConnectorAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating AccountConnector", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := DevConnectAccountConnectorSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateAccountConnectorRequest{
		Parent:             a.id.ParentString(),
		AccountConnectorId: a.id.ID(),
		AccountConnector:   resource,
	}
	op, err := a.gcpClient.CreateAccountConnector(ctx, req)
	if err != nil {
		return fmt.Errorf("creating AccountConnector %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create AccountConnector %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created AccountConnector", "name", a.id)

	status := &krm.DevConnectAccountConnectorStatus{}
	status.ObservedState = DevConnectAccountConnectorObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *AccountConnectorAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating AccountConnector", "name", a.id)

	desired, diffs, updateMask, err := compareAccountConnector(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateAccountConnectorRequest{
			AccountConnector: desired,
			UpdateMask:       updateMask,
		}
		op, err := a.gcpClient.UpdateAccountConnector(ctx, req)
		if err != nil {
			return fmt.Errorf("updating AccountConnector %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update AccountConnector %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated AccountConnector", "name", a.id)

		status := &krm.DevConnectAccountConnectorStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = DevConnectAccountConnectorObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareAccountConnector(ctx context.Context, u *unstructured.Unstructured, actual *pb.AccountConnector, desiredSpec *krm.DevConnectAccountConnectorSpec) (*pb.AccountConnector, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	return common.CompareGreenfieldSpecAndLabels(
		ctx,
		u,
		desiredSpec,
		actual,
		"labels",
		DevConnectAccountConnectorSpec_FromProto,
		DevConnectAccountConnectorSpec_ToProto,
		nil,
	)
}

func (a *AccountConnectorAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.DevConnectAccountConnector{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(DevConnectAccountConnectorSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = &a.id.Location
	obj.Spec.ResourceID = direct.LazyPtr(a.id.Account_connector)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.DevConnectAccountConnectorGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *AccountConnectorAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting AccountConnector", "name", a.id)

	req := &pb.DeleteAccountConnectorRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteAccountConnector(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting AccountConnector %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted AccountConnector", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete AccountConnector %s: %w", a.id, err)
		}
	}
	return true, nil
}
