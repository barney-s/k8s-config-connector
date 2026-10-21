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

package beyondcorpclientconnectorservice

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/beyondcorp/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/beyondcorp/clientconnectorservices/apiv1"
	pb "cloud.google.com/go/beyondcorp/clientconnectorservices/apiv1/clientconnectorservicespb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.BeyondCorpClientConnectorServiceGVK, NewClientConnectorServiceModel)
}

func NewClientConnectorServiceModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelClientConnectorService{config: *config}, nil
}

var _ directbase.Model = &modelClientConnectorService{}
var _ = strings.TrimPrefix

type modelClientConnectorService struct {
	config config.ControllerConfig
}

func (m *modelClientConnectorService) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.BeyondCorpClientConnectorService{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.BeyondCorpClientConnectorServiceIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ClientConnectorServiceAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelClientConnectorService) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelClientConnectorService) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.GRPCClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ClientConnectorService client: %w", err)
	}
	return gcpClient, err
}

type ClientConnectorServiceAdapter struct {
	id        *krm.BeyondCorpClientConnectorServiceIdentity
	gcpClient *gcp.Client
	desired   *krm.BeyondCorpClientConnectorService
	actual    *pb.ClientConnectorService
}

var _ directbase.Adapter = &ClientConnectorServiceAdapter{}

func (a *ClientConnectorServiceAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ClientConnectorService", "name", a.id)

	req := &pb.GetClientConnectorServiceRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetClientConnectorService(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ClientConnectorService %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ClientConnectorServiceAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ClientConnectorService", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := BeyondCorpClientConnectorServiceSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateClientConnectorServiceRequest{
		Parent:                   a.id.ParentString(),
		ClientConnectorServiceId: a.id.ClientConnectorService,
		ClientConnectorService:   resource,
	}
	op, err := a.gcpClient.CreateClientConnectorService(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ClientConnectorService %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create ClientConnectorService %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ClientConnectorService", "name", a.id)

	status := &krm.BeyondCorpClientConnectorServiceStatus{}
	status.ObservedState = BeyondCorpClientConnectorServiceObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ClientConnectorServiceAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ClientConnectorService", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := BeyondCorpClientConnectorServiceSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareClientConnectorService(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateClientConnectorServiceRequest{
			ClientConnectorService: desired,
			UpdateMask:             updateMask,
		}
		op, err := a.gcpClient.UpdateClientConnectorService(ctx, req)
		if err != nil {
			return fmt.Errorf("updating ClientConnectorService %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update ClientConnectorService %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated ClientConnectorService", "name", a.id)

		status := &krm.BeyondCorpClientConnectorServiceStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = BeyondCorpClientConnectorServiceObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareClientConnectorService(ctx context.Context, actual, desired *pb.ClientConnectorService) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, BeyondCorpClientConnectorServiceSpec_FromProto, BeyondCorpClientConnectorServiceSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *ClientConnectorServiceAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.BeyondCorpClientConnectorService{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(BeyondCorpClientConnectorServiceSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = a.id.Location

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ClientConnectorService)
	u.SetGroupVersionKind(krm.BeyondCorpClientConnectorServiceGVK)
	return u, nil
}

func (a *ClientConnectorServiceAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting ClientConnectorService", "name", a.id)

	req := &pb.DeleteClientConnectorServiceRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteClientConnectorService(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting ClientConnectorService %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted ClientConnectorService", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete ClientConnectorService %s: %w", a.id, err)
		}
	}
	return true, nil
}
