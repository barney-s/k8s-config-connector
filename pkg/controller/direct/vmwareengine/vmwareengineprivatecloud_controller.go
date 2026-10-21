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

package vmwareengine

import (
	"context"
	"fmt"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/vmwareengine/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/vmwareengine/apiv1"
	pb "cloud.google.com/go/vmwareengine/apiv1/vmwareenginepb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.VMwareEnginePrivateCloudGVK, NewPrivateCloudModel)
}

func NewPrivateCloudModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelPrivateCloud{config: *config}, nil
}

var _ directbase.Model = &modelPrivateCloud{}
var _ = strings.TrimPrefix

type modelPrivateCloud struct {
	config config.ControllerConfig
}

func (m *modelPrivateCloud) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.VMwareEnginePrivateCloud{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.PrivateCloudIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &PrivateCloudAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelPrivateCloud) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelPrivateCloud) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building PrivateCloud client: %w", err)
	}
	return gcpClient, err
}

type PrivateCloudAdapter struct {
	id        *krm.PrivateCloudIdentity
	gcpClient *gcp.Client
	desired   *krm.VMwareEnginePrivateCloud
	actual    *pb.PrivateCloud
}

var _ directbase.Adapter = &PrivateCloudAdapter{}

func (a *PrivateCloudAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting PrivateCloud", "name", a.id)

	req := &pb.GetPrivateCloudRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetPrivateCloud(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting PrivateCloud %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *PrivateCloudAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating PrivateCloud", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := VMwareEnginePrivateCloudSpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreatePrivateCloudRequest{
		Parent:         a.id.Parent().String(),
		PrivateCloudId: a.id.ID(),
		PrivateCloud:   resource,
	}
	op, err := a.gcpClient.CreatePrivateCloud(ctx, req)
	if err != nil {
		return fmt.Errorf("creating PrivateCloud %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create PrivateCloud %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created PrivateCloud", "name", a.id)

	status := &krm.VMwareEnginePrivateCloudStatus{}
	status.ObservedState = VMwareEnginePrivateCloudObservedState_v1alpha1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *PrivateCloudAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating PrivateCloud", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := VMwareEnginePrivateCloudSpec_v1alpha1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := comparePrivateCloud(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdatePrivateCloudRequest{
			PrivateCloud: desired,
			UpdateMask:   updateMask,
		}
		op, err := a.gcpClient.UpdatePrivateCloud(ctx, req)
		if err != nil {
			return fmt.Errorf("updating PrivateCloud %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update PrivateCloud %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated PrivateCloud", "name", a.id)

		status := &krm.VMwareEnginePrivateCloudStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = VMwareEnginePrivateCloudObservedState_v1alpha1_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func comparePrivateCloud(ctx context.Context, actual, desired *pb.PrivateCloud) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, VMwareEnginePrivateCloudSpec_v1alpha1_FromProto, VMwareEnginePrivateCloudSpec_v1alpha1_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *PrivateCloudAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.VMwareEnginePrivateCloud{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(VMwareEnginePrivateCloudSpec_v1alpha1_FromProto(mapCtx, a.actual))
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
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.VMwareEnginePrivateCloudGVK)
	return u, nil
}

func (a *PrivateCloudAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting PrivateCloud", "name", a.id)

	req := &pb.DeletePrivateCloudRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeletePrivateCloud(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting PrivateCloud %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted PrivateCloud", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete PrivateCloud %s: %w", a.id, err)
		}
	}
	return true, nil
}
