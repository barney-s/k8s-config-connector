// Copyright 2024 Google LLC
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

// +tool:controller-client
// proto.service: google.dataflow.v1beta3.TemplatesService
// proto.service: google.dataflow.v1beta3.JobsV1Beta3

package dataflow

import (
	"context"
	"fmt"

	api "cloud.google.com/go/dataflow/apiv1beta3"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/config"
)

type gcpClient struct {
	config config.ControllerConfig
}

func newGCPClient(ctx context.Context, config *config.ControllerConfig) (*gcpClient, error) {
	gcpClient := &gcpClient{
		config: *config,
	}
	return gcpClient, nil
}

func (m *gcpClient) newFlexTemplatesClient(ctx context.Context) (*api.FlexTemplatesClient, error) {
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}
	client, err := api.NewFlexTemplatesRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building dataflow flexTemplates client: %w", err)
	}
	return client, err
}

func (m *gcpClient) newJobsClient(ctx context.Context) (*api.JobsV1Beta3Client, error) {
	opts, err := m.config.RESTClientOptions()
	if err != nil {
		return nil, err
	}
	client, err := api.NewJobsV1Beta3RESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("building dataflow jobs client: %w", err)
	}
	return client, err
}
