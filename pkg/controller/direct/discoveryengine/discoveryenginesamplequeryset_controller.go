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

package discoveryengine

import (
	"context"
	"fmt"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/discoveryengine/v1alpha1"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/discoveryengine/apiv1beta"
	discoveryenginepb "cloud.google.com/go/discoveryengine/apiv1beta/discoveryenginepb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.DiscoveryEngineSampleQuerySetGVK, NewSampleQuerySetModel)
}

func NewSampleQuerySetModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelSampleQuerySet{config: *config}, nil
}

var _ directbase.Model = &modelSampleQuerySet{}
var _ = strings.TrimPrefix

type modelSampleQuerySet struct {
	config config.ControllerConfig
}

func (m *modelSampleQuerySet) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.DiscoveryEngineSampleQuerySet{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.DiscoveryEngineSampleQuerySetIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &SampleQuerySetAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelSampleQuerySet) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	id := &krm.DiscoveryEngineSampleQuerySetIdentity{}
	if err := id.FromExternal(url); err != nil {
		return nil, nil
	}
	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &SampleQuerySetAdapter{
		id:        id,
		gcpClient: gcpClient,
	}, nil
}

func (m *modelSampleQuerySet) client(ctx context.Context) (*gcp.SampleQuerySetClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewSampleQuerySetRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building SampleQuerySet client: %w", err)
	}
	return gcpClient, err
}

type SampleQuerySetAdapter struct {
	id        *krm.DiscoveryEngineSampleQuerySetIdentity
	gcpClient *gcp.SampleQuerySetClient
	desired   *krm.DiscoveryEngineSampleQuerySet
	actual    *discoveryenginepb.SampleQuerySet
}

var _ directbase.Adapter = &SampleQuerySetAdapter{}

func (a *SampleQuerySetAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting SampleQuerySet", "name", a.id)

	req := &discoveryenginepb.GetSampleQuerySetRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetSampleQuerySet(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting SampleQuerySet %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *SampleQuerySetAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating SampleQuerySet", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := DiscoveryEngineSampleQuerySetSpec_v1alpha1_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &discoveryenginepb.CreateSampleQuerySetRequest{
		Parent:           a.id.ParentString(),
		SampleQuerySetId: a.id.SampleQuerySet,
		SampleQuerySet:   resource,
	}
	created, err := a.gcpClient.CreateSampleQuerySet(ctx, req)
	if err != nil {
		return fmt.Errorf("creating SampleQuerySet %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created SampleQuerySet", "name", a.id)

	status := &krm.DiscoveryEngineSampleQuerySetStatus{}
	status.ObservedState = DiscoveryEngineSampleQuerySetObservedState_v1alpha1_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *SampleQuerySetAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating SampleQuerySet", "name", a.id)

	desired, diffs, updateMask, err := compareSampleQuerySet(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &discoveryenginepb.UpdateSampleQuerySetRequest{
			SampleQuerySet: desired,
			UpdateMask:     updateMask,
		}
		updated, err := a.gcpClient.UpdateSampleQuerySet(ctx, req)
		if err != nil {
			return fmt.Errorf("updating SampleQuerySet %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated SampleQuerySet", "name", a.id)

		status := &krm.DiscoveryEngineSampleQuerySetStatus{}
		mapCtx := &direct.MapContext{}
		status.ObservedState = DiscoveryEngineSampleQuerySetObservedState_v1alpha1_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareSampleQuerySet(ctx context.Context, u *unstructured.Unstructured, actual *discoveryenginepb.SampleQuerySet, desiredSpec *krm.DiscoveryEngineSampleQuerySetSpec) (*discoveryenginepb.SampleQuerySet, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		DiscoveryEngineSampleQuerySetSpec_v1alpha1_FromProto,
		DiscoveryEngineSampleQuerySetSpec_v1alpha1_ToProto,
		nil,
	)
}

func (a *SampleQuerySetAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.DiscoveryEngineSampleQuerySet{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(DiscoveryEngineSampleQuerySetSpec_v1alpha1_FromProto(mapCtx, a.actual))
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
	u.SetName(a.id.SampleQuerySet)
	u.SetGroupVersionKind(krm.DiscoveryEngineSampleQuerySetGVK)
	return u, nil
}

func (a *SampleQuerySetAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting SampleQuerySet", "name", a.id)

	req := &discoveryenginepb.DeleteSampleQuerySetRequest{
		Name: a.id.String(),
	}
	err := a.gcpClient.DeleteSampleQuerySet(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting SampleQuerySet %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted SampleQuerySet", "name", a.id)
	return true, nil
}
