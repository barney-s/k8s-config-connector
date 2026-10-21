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

package certificatemanager

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/certificatemanager/v1beta1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
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

	gcp "cloud.google.com/go/certificatemanager/apiv1"
	pb "cloud.google.com/go/certificatemanager/apiv1/certificatemanagerpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.CertificateManagerCertificateMapEntryGVK, NewCertificateMapEntryModel)
}

func NewCertificateMapEntryModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelCertificateMapEntry{config: *config}, nil
}

var _ directbase.Model = &modelCertificateMapEntry{}
var _ = strings.TrimPrefix

type modelCertificateMapEntry struct {
	config config.ControllerConfig
}

func (m *modelCertificateMapEntry) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.CertificateManagerCertificateMapEntry{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.CertificateManagerCertificateMapEntryIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &CertificateMapEntryAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelCertificateMapEntry) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.CertificateManagerCertificateMapEntryIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &CertificateMapEntryAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelCertificateMapEntry) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building CertificateMapEntry client: %w", err)
	}
	return gcpClient, err
}

type CertificateMapEntryAdapter struct {
	id        *krm.CertificateManagerCertificateMapEntryIdentity
	gcpClient *gcp.Client
	desired   *krm.CertificateManagerCertificateMapEntry
	actual    *pb.CertificateMapEntry
}

var _ directbase.Adapter = &CertificateMapEntryAdapter{}

func (a *CertificateMapEntryAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting CertificateMapEntry", "name", a.id)

	req := &pb.GetCertificateMapEntryRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetCertificateMapEntry(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting CertificateMapEntry %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *CertificateMapEntryAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating CertificateMapEntry", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := CertificateManagerCertificateMapEntrySpec_v1beta1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	resource.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	req := &pb.CreateCertificateMapEntryRequest{
		Parent:                a.id.ParentString(),
		CertificateMapEntryId: a.id.CertificateMapEntry,
		CertificateMapEntry:   resource,
	}
	op, err := a.gcpClient.CreateCertificateMapEntry(ctx, req)
	if err != nil {
		return fmt.Errorf("creating CertificateMapEntry %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create CertificateMapEntry %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created CertificateMapEntry", "name", a.id)

	status := &krm.CertificateManagerCertificateMapEntryStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *CertificateMapEntryAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating CertificateMapEntry", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := CertificateManagerCertificateMapEntrySpec_v1beta1_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	desired.Labels = label.NewGCPLabelsFromK8sLabels(a.desired.GetObjectMeta().GetLabels())

	diffs, updateMask, err := compareCertificateMapEntry(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateCertificateMapEntryRequest{
			CertificateMapEntry: desired,
			UpdateMask:          updateMask,
		}
		op, err := a.gcpClient.UpdateCertificateMapEntry(ctx, req)
		if err != nil {
			return fmt.Errorf("updating CertificateMapEntry %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update CertificateMapEntry %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated CertificateMapEntry", "name", a.id)

		status := &krm.CertificateManagerCertificateMapEntryStatus{}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareCertificateMapEntry(ctx context.Context, actual, desired *pb.CertificateMapEntry) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, CertificateManagerCertificateMapEntrySpec_v1beta1_FromProto, CertificateManagerCertificateMapEntrySpec_v1beta1_ToProto)
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

func (a *CertificateMapEntryAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.CertificateManagerCertificateMapEntry{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(CertificateManagerCertificateMapEntrySpec_v1beta1_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.ResourceID = direct.LazyPtr(a.id.CertificateMapEntry)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.CertificateMapEntry)
	u.SetGroupVersionKind(krm.CertificateManagerCertificateMapEntryGVK)
	export.SetLabels(u, a.actual.Labels)
	return u, nil
}

func (a *CertificateMapEntryAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting CertificateMapEntry", "name", a.id)

	req := &pb.DeleteCertificateMapEntryRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteCertificateMapEntry(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting CertificateMapEntry %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted CertificateMapEntry", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete CertificateMapEntry %s: %w", a.id, err)
		}
	}
	return true, nil
}
