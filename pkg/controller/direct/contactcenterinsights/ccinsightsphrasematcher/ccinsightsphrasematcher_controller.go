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

package ccinsightsphrasematcher

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/contactcenterinsights/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/contactcenterinsights/apiv1"
	pb "cloud.google.com/go/contactcenterinsights/apiv1/contactcenterinsightspb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.CCInsightsPhraseMatcherGVK, NewPhraseMatcherModel)
}

func NewPhraseMatcherModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelPhraseMatcher{config: *config}, nil
}

var _ directbase.Model = &modelPhraseMatcher{}
var _ = strings.TrimPrefix

type modelPhraseMatcher struct {
	config config.ControllerConfig
}

func (m *modelPhraseMatcher) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.CCInsightsPhraseMatcher{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.CCInsightsPhraseMatcherIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &PhraseMatcherAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelPhraseMatcher) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.CCInsightsPhraseMatcherIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &PhraseMatcherAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelPhraseMatcher) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building PhraseMatcher client: %w", err)
	}
	return gcpClient, err
}

type PhraseMatcherAdapter struct {
	id        *krm.CCInsightsPhraseMatcherIdentity
	gcpClient *gcp.Client
	desired   *krm.CCInsightsPhraseMatcher
	actual    *pb.PhraseMatcher
}

var _ directbase.Adapter = &PhraseMatcherAdapter{}

func (a *PhraseMatcherAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting PhraseMatcher", "name", a.id)

	req := &pb.GetPhraseMatcherRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetPhraseMatcher(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting PhraseMatcher %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *PhraseMatcherAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating PhraseMatcher", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := CCInsightsPhraseMatcherSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreatePhraseMatcherRequest{
		Parent:          a.id.ParentString(),
		PhraseMatcherId: a.id.FromExternal,
		PhraseMatcher:   resource,
	}
	created, err := a.gcpClient.CreatePhraseMatcher(ctx, req)
	if err != nil {
		return fmt.Errorf("creating PhraseMatcher %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created PhraseMatcher", "name", a.id)

	status := &krm.CCInsightsPhraseMatcherStatus{}
	status.ObservedState = CCInsightsPhraseMatcherObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *PhraseMatcherAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating PhraseMatcher", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := CCInsightsPhraseMatcherSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := comparePhraseMatcher(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdatePhraseMatcherRequest{
			PhraseMatcher: desired,
			UpdateMask:    updateMask,
		}
		updated, err := a.gcpClient.UpdatePhraseMatcher(ctx, req)
		if err != nil {
			return fmt.Errorf("updating PhraseMatcher %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated PhraseMatcher", "name", a.id)

		status := &krm.CCInsightsPhraseMatcherStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = CCInsightsPhraseMatcherObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func comparePhraseMatcher(ctx context.Context, actual, desired *pb.PhraseMatcher) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, CCInsightsPhraseMatcherSpec_FromProto, CCInsightsPhraseMatcherSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *PhraseMatcherAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.CCInsightsPhraseMatcher{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(CCInsightsPhraseMatcherSpec_FromProto(mapCtx, a.actual))
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
	u.SetName(a.id.FromExternal)
	u.SetGroupVersionKind(krm.CCInsightsPhraseMatcherGVK)
	return u, nil
}

func (a *PhraseMatcherAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting PhraseMatcher", "name", a.id)

	req := &pb.DeletePhraseMatcherRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeletePhraseMatcher(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting PhraseMatcher %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted PhraseMatcher", "name", a.id)
	return true, nil
}
