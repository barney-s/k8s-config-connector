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

package networkmanagement

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/networkmanagement/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/networkmanagement/apiv1"
	pb "cloud.google.com/go/networkmanagement/apiv1/networkmanagementpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.NetworkManagementConnectivityTestGVK, NewConnectivityTestModel)
}

func NewConnectivityTestModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelConnectivityTest{config: *config}, nil
}

var _ directbase.Model = &modelConnectivityTest{}
var _ = strings.TrimPrefix

type modelConnectivityTest struct {
	config config.ControllerConfig
}

func (m *modelConnectivityTest) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.NetworkManagementConnectivityTest{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.ConnectivityTestIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ConnectivityTestAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelConnectivityTest) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelConnectivityTest) client(ctx context.Context) (*gcp.ReachabilityClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building ConnectivityTest client: %w", err)
	}
	return gcpClient, err
}

type ConnectivityTestAdapter struct {
	id        *krm.ConnectivityTestIdentity
	gcpClient *gcp.ReachabilityClient
	desired   *krm.NetworkManagementConnectivityTest
	actual    *pb.ConnectivityTest
}

var _ directbase.Adapter = &ConnectivityTestAdapter{}

func (a *ConnectivityTestAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting ConnectivityTest", "name", a.id)

	req := &pb.GetConnectivityTestRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetConnectivityTest(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting ConnectivityTest %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ConnectivityTestAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating ConnectivityTest", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := NetworkManagementConnectivityTestSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateConnectivityTestRequest{
		Parent:           a.id.Parent().String(),
		TestId:           a.id.ID(),
		ConnectivityTest: resource,
	}
	op, err := a.gcpClient.CreateConnectivityTest(ctx, req)
	if err != nil {
		return fmt.Errorf("creating ConnectivityTest %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create ConnectivityTest %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created ConnectivityTest", "name", a.id)

	status := &krm.NetworkManagementConnectivityTestStatus{}
	status.ObservedState = NetworkManagementConnectivityTestObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ConnectivityTestAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating ConnectivityTest", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := NetworkManagementConnectivityTestSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareConnectivityTest(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateConnectivityTestRequest{
			ConnectivityTest: desired,
			UpdateMask:       updateMask,
		}
		op, err := a.gcpClient.UpdateConnectivityTest(ctx, req)
		if err != nil {
			return fmt.Errorf("updating ConnectivityTest %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update ConnectivityTest %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated ConnectivityTest", "name", a.id)

		status := &krm.NetworkManagementConnectivityTestStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = NetworkManagementConnectivityTestObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareConnectivityTest(ctx context.Context, actual, desired *pb.ConnectivityTest) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, NetworkManagementConnectivityTestSpec_FromProto, NetworkManagementConnectivityTestSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *ConnectivityTestAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.NetworkManagementConnectivityTest{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(NetworkManagementConnectivityTestSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.ID())
	u.SetGroupVersionKind(krm.NetworkManagementConnectivityTestGVK)
	return u, nil
}

func (a *ConnectivityTestAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting ConnectivityTest", "name", a.id)

	req := &pb.DeleteConnectivityTestRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteConnectivityTest(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting ConnectivityTest %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted ConnectivityTest", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete ConnectivityTest %s: %w", a.id, err)
		}
	}
	return true, nil
}
