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

package videostitcher

import (
	"context"
	"fmt"
	refs "github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	krm "github.com/GoogleCloudPlatform/k8s-config-connector/apis/videostitcher/v1alpha1"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/common"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/directbase"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct/registry"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/structuredreporting"
	"strings"

	gcp "cloud.google.com/go/video/stitcher/apiv1"
	pb "cloud.google.com/go/video/stitcher/apiv1/stitcherpb"
	"google.golang.org/api/option"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

func init() {
	registry.RegisterModel(krm.VideoStitcherCDNKeyGVK, NewCdnKeyModel)
}

func NewCdnKeyModel(ctx context.Context, config *config.ControllerConfig) (directbase.Model, error) {
	return &modelCdnKey{config: *config}, nil
}

var _ directbase.Model = &modelCdnKey{}
var _ = strings.TrimPrefix

type modelCdnKey struct {
	config config.ControllerConfig
}

func (m *modelCdnKey) AdapterForObject(ctx context.Context, op *directbase.AdapterForObjectOperation) (directbase.Adapter, error) {
	u := op.GetUnstructured()
	reader := op.Reader
	_ = reader
	obj := &krm.VideoStitcherCDNKey{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &obj); err != nil {
		return nil, fmt.Errorf("error converting to %T: %w", obj, err)
	}
	i, err := obj.GetIdentity(ctx, reader)
	if err != nil {
		return nil, err
	}
	id := i.(*krm.VideoStitcherCDNKeyIdentity)
	if err := common.NormalizeReferences(ctx, reader, obj, nil); err != nil {
		return nil, fmt.Errorf("normalizing references: %w", err)
	}

	gcpClient, err := m.client(ctx)
	if err != nil {
		return nil, err
	}
	return &CdnKeyAdapter{
		id:        id,
		gcpClient: gcpClient,
		desired:   obj,
	}, nil
}

func (m *modelCdnKey) AdapterForURL(ctx context.Context, url string) (directbase.Adapter, error) {
	// TODO: Support URLs
	return nil, nil
}

func (m *modelCdnKey) client(ctx context.Context) (*gcp.VideoStitcherClient, error) {
	var opts []option.ClientOption
	opts, err := m.config.GRPCClientOptions()
	if err != nil {
		return nil, err
	}

	gcpClient, err := gcp.NewVideoStitcherClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building CdnKey client: %w", err)
	}
	return gcpClient, err
}

type CdnKeyAdapter struct {
	id        *krm.VideoStitcherCDNKeyIdentity
	gcpClient *gcp.VideoStitcherClient
	desired   *krm.VideoStitcherCDNKey
	actual    *pb.CdnKey
}

var _ directbase.Adapter = &CdnKeyAdapter{}

func (a *CdnKeyAdapter) Find(ctx context.Context) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("getting CdnKey", "name", a.id)

	req := &pb.GetCdnKeyRequest{
		Name: a.id.String(),
	}
	resp, err := a.gcpClient.GetCdnKey(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("getting CdnKey %q: %w", a.id, err)
	}

	a.actual = resp
	return true, nil
}

func (a *CdnKeyAdapter) Create(ctx context.Context, createOp *directbase.CreateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("creating CdnKey", "name", a.id)
	mapCtx := &direct.MapContext{}

	desired := a.desired.DeepCopy()
	resource := VideoStitcherCDNKeySpec_ToProto(mapCtx, &desired.Spec)
	if mapCtx.Err() != nil {
		return mapCtx.Err()
	}

	req := &pb.CreateCdnKeyRequest{
		Parent:   a.id.ParentString(),
		CdnKeyId: a.id.CdnKey,
		CdnKey:   resource,
	}
	op, err := a.gcpClient.CreateCdnKey(ctx, req)
	if err != nil {
		return fmt.Errorf("creating CdnKey %s: %w", a.id, err)
	}
	_, err = op.Wait(ctx)
	if err != nil {
		return fmt.Errorf("waiting create CdnKey %s: %w", a.id, err)
	}
	log.V(2).Info("successfully created CdnKey", "name", a.id)

	status := &krm.VideoStitcherCDNKeyStatus{}
	status.ExternalRef = direct.LazyPtr(a.id.String())
	return createOp.UpdateStatus(ctx, status, nil)
}

func (a *CdnKeyAdapter) Update(ctx context.Context, updateOp *directbase.UpdateOperation) error {
	log := klog.FromContext(ctx)
	log.V(2).Info("updating CdnKey", "name", a.id)

	desired, diffs, updateMask, err := compareCdnKey(ctx, updateOp.GetUnstructured(), a.actual, &a.desired.Spec)
	if err != nil {
		return err
	}
	if diffs.HasDiff() {
		diffs.Object = updateOp.GetUnstructured()
		structuredreporting.ReportDiff(ctx, diffs)

		if desired != nil {
			desired.Name = a.id.String()
		}

		req := &pb.UpdateCdnKeyRequest{
			CdnKey:     desired,
			UpdateMask: updateMask,
		}
		op, err := a.gcpClient.UpdateCdnKey(ctx, req)
		if err != nil {
			return fmt.Errorf("updating CdnKey %s: %w", a.id, err)
		}
		_, err = op.Wait(ctx)
		if err != nil {
			return fmt.Errorf("waiting update CdnKey %s: %w", a.id, err)
		}
		log.V(2).Info("successfully updated CdnKey", "name", a.id)

		status := &krm.VideoStitcherCDNKeyStatus{}
		status.ExternalRef = direct.LazyPtr(a.id.String())
		return updateOp.UpdateStatus(ctx, status, nil)
	}

	return nil
}

func compareCdnKey(ctx context.Context, u *unstructured.Unstructured, actual *pb.CdnKey, desiredSpec *krm.VideoStitcherCDNKeySpec) (*pb.CdnKey, *structuredreporting.Diff, *fieldmaskpb.FieldMask, error) {
	_ = u
	return common.CompareGreenfieldSpec(
		ctx,
		desiredSpec,
		actual,
		VideoStitcherCDNKeySpec_FromProto,
		VideoStitcherCDNKeySpec_ToProto,
		nil,
	)
}

func (a *CdnKeyAdapter) Export(ctx context.Context) (*unstructured.Unstructured, error) {
	if a.actual == nil {
		return nil, fmt.Errorf("Find() not called")
	}
	u := &unstructured.Unstructured{}

	obj := &krm.VideoStitcherCDNKey{}
	mapCtx := &direct.MapContext{}
	obj.Spec = direct.ValueOf(VideoStitcherCDNKeySpec_FromProto(mapCtx, a.actual))
	if mapCtx.Err() != nil {
		return nil, mapCtx.Err()
	}
	obj.Spec.ProjectRef = &refs.ProjectRef{External: a.id.Project}
	obj.Spec.Location = a.id.Location
	obj.Spec.ResourceID = direct.LazyPtr(a.id.CdnKey)

	uObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	u.Object = uObj
	u.SetName(a.id.CdnKey)
	u.SetGroupVersionKind(krm.VideoStitcherCDNKeyGVK)
	return u, nil
}

func (a *CdnKeyAdapter) Delete(ctx context.Context, deleteOp *directbase.DeleteOperation) (bool, error) {
	log := klog.FromContext(ctx)
	log.V(2).Info("deleting CdnKey", "name", a.id)

	req := &pb.DeleteCdnKeyRequest{
		Name: a.id.String(),
	}
	op, err := a.gcpClient.DeleteCdnKey(ctx, req)
	if err != nil {
		if direct.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("deleting CdnKey %s: %w", a.id, err)
	}
	log.V(2).Info("successfully deleted CdnKey", "name", a.id)
	err = op.Wait(ctx)
	if err != nil {
		if !direct.IsNotFound(err) && !strings.Contains(err.Error(), "missing \"value\" field") {
			return false, fmt.Errorf("waiting delete CdnKey %s: %w", a.id, err)
		}
	}
	return true, nil
}
