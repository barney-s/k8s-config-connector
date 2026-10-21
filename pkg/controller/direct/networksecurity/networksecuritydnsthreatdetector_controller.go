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

package networksecurity

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/networksecurity/v1alpha1"
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

	gcp "cloud.google.com/go/networksecurity/apiv1"
	pb "cloud.google.com/go/networksecurity/apiv1/networksecuritypb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.NetworkSecurityDNSThreatDetectorGVK, NewDnsThreatDetectorModel)
}

func NewDnsThreatDetectorModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelDnsThreatDetector{config: *config}, nil
}

var _ directbase.Model = &modelDnsThreatDetector{}
var _ = strings.TrimPrefix

type modelDnsThreatDetector struct {
	config config.ControllerConfig
}

func (m *modelDnsThreatDetector) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.NetworkSecurityDNSThreatDetector{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.NetworkSecurityDNSThreatDetectorIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &DnsThreatDetectorAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelDnsThreatDetector) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelDnsThreatDetector) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.GRPCClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building DnsThreatDetector client: %w", err)
	}
	return gcpClient, err
}

type DnsThreatDetectorAdapter struct {
	id        *krm.NetworkSecurityDNSThreatDetectorIdentity
	gcpClient *gcp.Client
	desired   *krm.NetworkSecurityDNSThreatDetector
	actual    *pb.DnsThreatDetector
}

var _ directbase.Adapter = &DnsThreatDetectorAdapter{}

func (a *DnsThreatDetectorAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting DnsThreatDetector", "name", a.id)

	req := &pb.GetDnsThreatDetectorRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetDnsThreatDetector(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting DnsThreatDetector %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *DnsThreatDetectorAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating DnsThreatDetector", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := NetworkSecurityDNSThreatDetectorSpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateDnsThreatDetectorRequest{
		Parent:              a.id.ParentString(),
		DnsThreatDetectorId: a.id.DNSThreatDetector,
		DnsThreatDetector:   resource,
	}
	created, err := a.gcpClient.CreateDnsThreatDetector(ctx, req)
	if err != nil {
		return fmt.Errorf("creating DnsThreatDetector %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created DnsThreatDetector", "name", a.id)

	status := &krm.NetworkSecurityDNSThreatDetectorStatus{}
	status.ObservedState = NetworkSecurityDNSThreatDetectorObservedState_v1alpha1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *DnsThreatDetectorAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating DnsThreatDetector", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := NetworkSecurityDNSThreatDetectorSpec_v1alpha1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareDnsThreatDetector(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateDnsThreatDetectorRequest{
			DnsThreatDetector: desired,
			UpdateMask:        updateMask,
		}
		updated, err := a.gcpClient.UpdateDnsThreatDetector(ctx, req)
		if err != nil {
			return fmt.Errorf("updating DnsThreatDetector %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated DnsThreatDetector", "name", a.id)

		status := &krm.NetworkSecurityDNSThreatDetectorStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = NetworkSecurityDNSThreatDetectorObservedState_v1alpha1_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareDnsThreatDetector(ctx context.Context, actual, desired *pb.DnsThreatDetector) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, NetworkSecurityDNSThreatDetectorSpec_v1alpha1_FromProto, NetworkSecurityDNSThreatDetectorSpec_v1alpha1_ToProto)
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

func (a *DnsThreatDetectorAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.NetworkSecurityDNSThreatDetector{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(NetworkSecurityDNSThreatDetectorSpec_v1alpha1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.DNSThreatDetector)
	u.SetGroupVersionKind(krm.NetworkSecurityDNSThreatDetectorGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *DnsThreatDetectorAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting DnsThreatDetector", "name", a.id)

	req := &pb.DeleteDnsThreatDetectorRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteDnsThreatDetector(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting DnsThreatDetector %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted DnsThreatDetector", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete DnsThreatDetector %s: %w", a.id, err)
		}
	}
	return true, nil
}
