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

package speech

import (
	"context"
	"fmt"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/speech/v1beta1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/mappers"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/speech/apiv2"
	pb "cloud.google.com/go/speech/apiv2/speechpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.SpeechPhraseSetGVK, NewPhraseSetModel)
}

func NewPhraseSetModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelPhraseSet{config: *config}, nil
}

var _ directbase.Model = &modelPhraseSet{}
var _ = strings.TrimPrefix

type modelPhraseSet struct {
	config config.ControllerConfig
}

func (m *modelPhraseSet) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.SpeechPhraseSet{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	id := &krm.PhraseSetIdentity{}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &PhraseSetAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelPhraseSet) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelPhraseSet) client(ctx context.Context) (*gcp.Client, error) {
	var opts []option.ClientOption
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building PhraseSet client: %w", err)
	}
	return gcpClient, err
}

type PhraseSetAdapter struct {
	id        *krm.PhraseSetIdentity
	gcpClient *gcp.Client
	desired   *krm.SpeechPhraseSet
	actual    *pb.PhraseSet
}

var _ directbase.Adapter = &PhraseSetAdapter{}

func (a *PhraseSetAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting PhraseSet", "name", a.id)

	req := &pb.GetPhraseSetRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetPhraseSet(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting PhraseSet %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *PhraseSetAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating PhraseSet", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := SpeechPhraseSetSpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreatePhraseSetRequest{
		Parent:      a.id.Parent().String(),
		PhraseSetId: a.id.ID(),
		PhraseSet:   resource,
	}
	op, err := a.gcpClient.CreatePhraseSet(ctx, req)
	if err != nil {
		return fmt.Errorf("creating PhraseSet %s: %w", a.id, err)
	}
	created, err := op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create PhraseSet %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created PhraseSet", "name", a.id)

	status := &krm.SpeechPhraseSetStatus{}
	status.ObservedState = SpeechPhraseSetObservedState_FromProto(mapCtx, created)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *PhraseSetAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating PhraseSet", "name", a.id)

	mapCtx := &direct.MapContext{}
	desired := SpeechPhraseSetSpec_ToProto(mapCtx, &a.desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	diffs, updateMask, err := comparePhraseSet(ctx, a.actual, desired)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		desired.Name = a.id.String()

		req := &pb.UpdatePhraseSetRequest{
			PhraseSet:  desired,
			UpdateMask: updateMask,
		}
		op, err := a.gcpClient.UpdatePhraseSet(ctx, req)
		if err != nil {
			return fmt.Errorf("updating PhraseSet %s: %w", a.id, err)
		}
		updated, err := op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update PhraseSet %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated PhraseSet", "name", a.id)

		status := &krm.SpeechPhraseSetStatus{}
		mapCtx = &direct.MapContext{}
		status.ObservedState = SpeechPhraseSetObservedState_FromProto(mapCtx, updated)
		if mapCtx.Err() != nil {
			return mapCtx.Err()
		}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func comparePhraseSet(ctx context.Context, actual, desired *pb.PhraseSet) (*structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	maskedActual, err := mappers.OnlySpecFields(actual, SpeechPhraseSetSpec_FromProto, SpeechPhraseSetSpec_ToProto)
	if err != nil {
		return nil, nil, err
	}
	diffs, updateMask, err := common.DiffForTopLevelFields(ctx, desired.ProtoReflect(), maskedActual.ProtoReflect())
	if err != nil {
		return nil, nil, err
	}
	return diffs, updateMask, nil
}

func (a *PhraseSetAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.SpeechPhraseSet{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(SpeechPhraseSetSpec_FromProto(mapCtx, a.actual))
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
	u.SetGroupVersionKind(krm.SpeechPhraseSetGVK)
	return u, nil
}

func (a *PhraseSetAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting PhraseSet", "name", a.id)

	req := &pb.DeletePhraseSetRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeletePhraseSet(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting PhraseSet %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted PhraseSet", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete PhraseSet %s: %w", a.id, err)
		}
	}
	return true, nil
}
