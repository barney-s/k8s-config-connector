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

// +tool:mockgcp-support
// proto.service: google.cloud.metastore.v1.DataprocMetastore
// proto.message: google.cloud.metastore.v1.Service

package mockmetastore

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/GoogleCloudPlatform/k8s-config-connector/mockgcp/common/projects"
	pb "github.com/GoogleCloudPlatform/k8s-config-connector/mockgcp/generated/mockgcp/cloud/metastore/v1"
	longrunningpb "google.golang.org/genproto/googleapis/longrunning"
)

func (s *dataprocMetastoreService) GetService(ctx context.Context, req *pb.GetServiceRequest) (*pb.Service, error) {
	name, err := s.parseServiceName(req.Name)
	if err != nil {
		return nil, err
	}

	fqn := name.String()

	obj := &pb.Service{}
	if err := s.storage.Get(ctx, fqn, obj); err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, status.Errorf(codes.NotFound, "Service %q not found", fqn)
		}
		return nil, err
	}

	return obj, nil
}

func (s *dataprocMetastoreService) CreateService(ctx context.Context, req *pb.CreateServiceRequest) (*longrunningpb.Operation, error) {
	reqName := req.Parent + "/services/" + req.ServiceId
	name, err := s.parseServiceName(reqName)
	if err != nil {
		return nil, err
	}

	fqn := name.String()

	obj := proto.Clone(req.Service).(*pb.Service)
	obj.Name = fqn

	now := time.Now()
	obj.CreateTime = timestamppb.New(now)
	obj.UpdateTime = timestamppb.New(now)
	obj.State = pb.Service_CREATING

	if err := s.storage.Create(ctx, fqn, obj); err != nil {
		return nil, err
	}

	// By default, immediately finish the LRO with success.
	lroPrefix := fmt.Sprintf("projects/%s/locations/%s", name.Project.ID, name.Location)
	lroMetadata := &pb.OperationMetadata{
		CreateTime: timestamppb.New(now),
		Target:     fqn,
		Verb:       "create",
		ApiVersion: "v1",
	}
	return s.operations.StartLRO(ctx, lroPrefix, lroMetadata, func() (proto.Message, error) {
		lroMetadata.EndTime = timestamppb.New(now)
		updated, err := s.updateService(ctx, fqn, func(obj *pb.Service) { obj.State = pb.Service_ACTIVE })
		return updated, err
	})
}

func (s *dataprocMetastoreService) UpdateService(ctx context.Context, req *pb.UpdateServiceRequest) (*longrunningpb.Operation, error) {
	name, err := s.parseServiceName(req.GetService().GetName())
	if err != nil {
		return nil, err
	}
	fqn := name.String()

	now := time.Now()

	update := func(obj *pb.Service) {
		proto.Merge(obj, req.Service)
		obj.UpdateTime = timestamppb.New(now)
	}

	updated, err := s.updateService(ctx, fqn, update)
	if err != nil {
		return nil, err
	}

	lroPrefix := fmt.Sprintf("projects/%s/locations/%s", name.Project.ID, name.Location)
	// // Returns with no createTime
	// lroRet := proto.Clone(obj).(*pb.Workflow)
	// lroRet.CreateTime = nil
	// lroRet.UpdateTime = nil
	// lroRet.RevisionCreateTime = nil

	lroMetadata := &pb.OperationMetadata{
		ApiVersion: "v1",
		CreateTime: timestamppb.New(now),
		Target:     name.String(),
		Verb:       "update",
	}
	return s.operations.StartLRO(ctx, lroPrefix, lroMetadata, func() (proto.Message, error) {
		lroMetadata.EndTime = timestamppb.Now()
		return updated, nil
	})
}

func (s *dataprocMetastoreService) DeleteService(ctx context.Context, req *pb.DeleteServiceRequest) (*longrunningpb.Operation, error) {
	name, err := s.parseServiceName(req.Name)
	if err != nil {
		return nil, err
	}

	fqn := name.String()

	deleted := &pb.Service{}
	if err := s.storage.Delete(ctx, fqn, deleted); err != nil {
		return nil, err
	}

	// By default, immediately finish the LRO with success.
	lroPrefix := fmt.Sprintf("projects/%s/locations/%s", name.Project.ID, name.Location)
	lroMetadata := &pb.OperationMetadata{
		ApiVersion: "v1",
		CreateTime: timestamppb.Now(),
		Target:     fqn,
		Verb:       "delete",
	}
	return s.operations.StartLRO(ctx, lroPrefix, lroMetadata, func() (proto.Message, error) {
		lroMetadata.EndTime = timestamppb.Now()
		return &emptypb.Empty{}, nil
	})
}

// updateService will read-modify-write the object with optimistic locking
func (s *dataprocMetastoreService) updateService(ctx context.Context, fqn string, update func(obj *pb.Service)) (*pb.Service, error) {
	obj := &pb.Service{}
	if err := s.storage.Get(ctx, fqn, obj); err != nil {
		return nil, err
	}

	update(obj)

	if err := s.storage.Update(ctx, fqn, obj); err != nil {
		return nil, err
	}

	return obj, nil
}

type serviceName struct {
	Project  *projects.ProjectData
	Location string
	Name     string
}

func (n *serviceName) String() string {
	return fmt.Sprintf("projects/%s/locations/%s/services/%s", n.Project.ID, n.Location, n.Name)
}

// parseServiceName parses a string into an serviceName.
// The expected form is `projects/*/locations/*/services/*`.
func (s *MockService) parseServiceName(name string) (*serviceName, error) {
	tokens := strings.Split(name, "/")

	if len(tokens) == 6 && tokens[0] == "projects" && tokens[2] == "locations" && tokens[4] == "services" {
		project, err := s.Projects.GetProjectByID(tokens[1])
		if err != nil {
			return nil, err
		}

		name := &serviceName{
			Project:  project,
			Location: tokens[3],
			Name:     tokens[5],
		}

		return name, nil
	}

	return nil, status.Errorf(codes.InvalidArgument, "name %q is not valid", name)
}

// buildServiceName builds a serviceName from the components.
func (s *MockService) buildServiceName(projectName, region, service string) (*serviceName, error) {
	project, err := s.Projects.GetProjectByID(projectName)
	if err != nil {
		return nil, err
	}

	return &serviceName{
		Project:  project,
		Location: region,
		Name:     service,
	}, nil
}

