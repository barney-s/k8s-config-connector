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

package gkemulticloud

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/gkemulticloud/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/gkemulticloud/apiv1"
	pb "cloud.google.com/go/gkemulticloud/apiv1/gkemulticloudpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.GKEMulticloudAttachedClusterGVK, NewAttachedClusterModel)
}

func NewAttachedClusterModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelAttachedCluster{config: *config}, nil
}

var _ directbase.Model = &modelAttachedCluster{}
var _ = strings.TrimPrefix

type modelAttachedCluster struct {
	config config.ControllerConfig
}

func (m *modelAttachedCluster) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.GKEMulticloudAttachedCluster{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.GKEMulticloudAttachedClusterIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &AttachedClusterAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelAttachedCluster) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.GKEMulticloudAttachedClusterIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &AttachedClusterAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelAttachedCluster) client(ctx context.Context) (*gcp.AttachedClustersClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.GRPCClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewAttachedClustersClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building AttachedCluster client: %w", err)
	}
	return gcpClient, err
}

type AttachedClusterAdapter struct {
	id        *krm.GKEMulticloudAttachedClusterIdentity
	gcpClient *gcp.AttachedClustersClient
	desired   *krm.GKEMulticloudAttachedCluster
	actual    *pb.AttachedCluster
}

var _ directbase.Adapter = &AttachedClusterAdapter{}

func (a *AttachedClusterAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting AttachedCluster", "name", a.id)

	req := &pb.GetAttachedClusterRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetAttachedCluster(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting AttachedCluster %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *AttachedClusterAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating AttachedCluster", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := GKEMulticloudAttachedClusterSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateAttachedClusterRequest{
		Parent:            a.id.ParentString(),
		AttachedClusterId: a.id.AttachedCluster,
		AttachedCluster:   resource,
	}
	op, err := a.gcpClient.CreateAttachedCluster(ctx, req)
	if err != nil {
		return fmt.Errorf("creating AttachedCluster %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create AttachedCluster %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created AttachedCluster", "name", a.id)

	status := &krm.GKEMulticloudAttachedClusterStatus{}
	status.ObservedState = GKEMulticloudAttachedClusterObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *AttachedClusterAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating AttachedCluster", "name", a.id)

	desired, diffs, updateMask, err := compareAttachedCluster(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateAttachedClusterRequest{
			AttachedCluster: desired,
			UpdateMask:      updateMask,
		}
		op, err := a.gcpClient.UpdateAttachedCluster(ctx, req)
		if err != nil {
			return fmt.Errorf("updating AttachedCluster %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update AttachedCluster %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated AttachedCluster", "name", a.id)

		status := &krm.GKEMulticloudAttachedClusterStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = GKEMulticloudAttachedClusterObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareAttachedCluster(ctx context.Context, u *unstructured.Unstructured, actual *pb.AttachedCluster, desiredSpec *krm.GKEMulticloudAttachedClusterSpec) (*pb.AttachedCluster, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		GKEMulticloudAttachedClusterSpec_FromProto,
		GKEMulticloudAttachedClusterSpec_ToProto,
		nil,
	)
}

func (a *AttachedClusterAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.GKEMulticloudAttachedCluster{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(GKEMulticloudAttachedClusterSpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = &a.id.Location
	obj.Spec.ResourceID = direct.LazyPtr(a.id.AttachedCluster)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.AttachedCluster)
	u.SetGroupVersionKind(krm.GKEMulticloudAttachedClusterGVK)
	return u, nil
}

func (a *AttachedClusterAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting AttachedCluster", "name", a.id)

	req := &pb.DeleteAttachedClusterRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteAttachedCluster(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting AttachedCluster %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted AttachedCluster", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete AttachedCluster %s: %w", a.id, err)
		}
	}
	return true, nil
}
