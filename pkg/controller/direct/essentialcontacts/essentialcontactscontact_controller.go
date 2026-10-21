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

package essentialcontacts

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/essentialcontacts/v1beta1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/essentialcontacts/apiv1"
	pb "cloud.google.com/go/essentialcontacts/apiv1/essentialcontactspb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.EssentialContactsContactGVK, NewContactModel)
}

func NewContactModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelContact{config: *config}, nil
}

var _ directbase.Model = &modelContact{}
var _ = strings.TrimPrefix

type modelContact struct {
	config config.ControllerConfig
}

func (m *modelContact) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.EssentialContactsContact{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.ContactIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &ContactAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelContact) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelContact) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building Contact client: %w", err)
	}
	return gcpClient, err
}

type ContactAdapter struct {
	id        *krm.ContactIdentity
	gcpClient *gcp.Client
	desired   *krm.EssentialContactsContact
	actual    *pb.Contact
}

var _ directbase.Adapter = &ContactAdapter{}

func (a *ContactAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting Contact", "name", a.id)

	req := &pb.GetContactRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetContact(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting Contact %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *ContactAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating Contact", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := EssentialContactsContactSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateContactRequest{
		Parent:    a.id.Parent().String(),
		ContactId: a.id.ID(),
		Contact:   resource,
	}
	created, err := a.gcpClient.CreateContact(ctx, req)
	if err != nil {
		return fmt.Errorf("creating Contact %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created Contact", "name", a.id)

	status := &krm.EssentialContactsContactStatus{}
	status.ObservedState = EssentialContactsContactObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *ContactAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating Contact", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := EssentialContactsContactSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := compareContact(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdateContactRequest{
			Contact:    desired,
			UpdateMask: updateMask,
		}
		updated, err := a.gcpClient.UpdateContact(ctx, req)
		if err != nil {
			return fmt.Errorf("updating Contact %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated Contact", "name", a.id)

		status := &krm.EssentialContactsContactStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = EssentialContactsContactObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareContact(ctx context.Context, actual, desired *pb.Contact) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, EssentialContactsContactSpec_FromProto, EssentialContactsContactSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *ContactAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.EssentialContactsContact{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(EssentialContactsContactSpec_FromProto(mapCtx, a.actual))
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
	u.SetGroupVersionKind(krm.EssentialContactsContactGVK)
	return u, nil
}

func (a *ContactAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting Contact", "name", a.id)

	req := &pb.DeleteContactRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteContact(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting Contact %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted Contact", "name", a.id)
	return true, nil
}
