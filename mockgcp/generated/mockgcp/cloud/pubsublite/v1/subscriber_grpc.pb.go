// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.2.0
// - protoc             v3.12.4
// source: mockgcp/cloud/pubsublite/v1/subscriber.proto

package pubsublitepb

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

// SubscriberServiceClient is the client API for SubscriberService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type SubscriberServiceClient interface {
	// Establishes a stream with the server for receiving messages.
	Subscribe(ctx context.Context, opts ...grpc.CallOption) (SubscriberService_SubscribeClient, error)
}

type subscriberServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewSubscriberServiceClient(cc grpc.ClientConnInterface) SubscriberServiceClient {
	return &subscriberServiceClient{cc}
}

func (c *subscriberServiceClient) Subscribe(ctx context.Context, opts ...grpc.CallOption) (SubscriberService_SubscribeClient, error) {
	stream, err := c.cc.NewStream(ctx, &SubscriberService_ServiceDesc.Streams[0], "/mockgcp.cloud.pubsublite.v1.SubscriberService/Subscribe", opts...)
	if err != nil {
		return nil, err
	}
	x := &subscriberServiceSubscribeClient{stream}
	return x, nil
}

type SubscriberService_SubscribeClient interface {
	Send(*SubscribeRequest) error
	Recv() (*SubscribeResponse, error)
	grpc.ClientStream
}

type subscriberServiceSubscribeClient struct {
	grpc.ClientStream
}

func (x *subscriberServiceSubscribeClient) Send(m *SubscribeRequest) error {
	return x.ClientStream.SendMsg(m)
}

func (x *subscriberServiceSubscribeClient) Recv() (*SubscribeResponse, error) {
	m := new(SubscribeResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// SubscriberServiceServer is the server API for SubscriberService service.
// All implementations must embed UnimplementedSubscriberServiceServer
// for forward compatibility
type SubscriberServiceServer interface {
	// Establishes a stream with the server for receiving messages.
	Subscribe(SubscriberService_SubscribeServer) error
	mustEmbedUnimplementedSubscriberServiceServer()
}

// UnimplementedSubscriberServiceServer must be embedded to have forward compatible implementations.
type UnimplementedSubscriberServiceServer struct {
}

func (UnimplementedSubscriberServiceServer) Subscribe(SubscriberService_SubscribeServer) error {
	return status.Errorf(codes.Unimplemented, "method Subscribe not implemented")
}
func (UnimplementedSubscriberServiceServer) mustEmbedUnimplementedSubscriberServiceServer() {}

// UnsafeSubscriberServiceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to SubscriberServiceServer will
// result in compilation errors.
type UnsafeSubscriberServiceServer interface {
	mustEmbedUnimplementedSubscriberServiceServer()
}

func RegisterSubscriberServiceServer(s grpc.ServiceRegistrar, srv SubscriberServiceServer) {
	s.RegisterService(&SubscriberService_ServiceDesc, srv)
}

func _SubscriberService_Subscribe_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(SubscriberServiceServer).Subscribe(&subscriberServiceSubscribeServer{stream})
}

type SubscriberService_SubscribeServer interface {
	Send(*SubscribeResponse) error
	Recv() (*SubscribeRequest, error)
	grpc.ServerStream
}

type subscriberServiceSubscribeServer struct {
	grpc.ServerStream
}

func (x *subscriberServiceSubscribeServer) Send(m *SubscribeResponse) error {
	return x.ServerStream.SendMsg(m)
}

func (x *subscriberServiceSubscribeServer) Recv() (*SubscribeRequest, error) {
	m := new(SubscribeRequest)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// SubscriberService_ServiceDesc is the grpc.ServiceDesc for SubscriberService service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var SubscriberService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "mockgcp.cloud.pubsublite.v1.SubscriberService",
	HandlerType: (*SubscriberServiceServer)(nil),
	Methods:     []grpc.MethodDesc{},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "Subscribe",
			Handler:       _SubscriberService_Subscribe_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
	},
	Metadata: "mockgcp/cloud/pubsublite/v1/subscriber.proto",
}

// PartitionAssignmentServiceClient is the client API for PartitionAssignmentService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type PartitionAssignmentServiceClient interface {
	// Assign partitions for this client to handle for the specified subscription.
	//
	// The client must send an InitialPartitionAssignmentRequest first.
	// The server will then send at most one unacknowledged PartitionAssignment
	// outstanding on the stream at a time.
	// The client should send a PartitionAssignmentAck after updating the
	// partitions it is connected to to reflect the new assignment.
	AssignPartitions(ctx context.Context, opts ...grpc.CallOption) (PartitionAssignmentService_AssignPartitionsClient, error)
}

type partitionAssignmentServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewPartitionAssignmentServiceClient(cc grpc.ClientConnInterface) PartitionAssignmentServiceClient {
	return &partitionAssignmentServiceClient{cc}
}

func (c *partitionAssignmentServiceClient) AssignPartitions(ctx context.Context, opts ...grpc.CallOption) (PartitionAssignmentService_AssignPartitionsClient, error) {
	stream, err := c.cc.NewStream(ctx, &PartitionAssignmentService_ServiceDesc.Streams[0], "/mockgcp.cloud.pubsublite.v1.PartitionAssignmentService/AssignPartitions", opts...)
	if err != nil {
		return nil, err
	}
	x := &partitionAssignmentServiceAssignPartitionsClient{stream}
	return x, nil
}

type PartitionAssignmentService_AssignPartitionsClient interface {
	Send(*PartitionAssignmentRequest) error
	Recv() (*PartitionAssignment, error)
	grpc.ClientStream
}

type partitionAssignmentServiceAssignPartitionsClient struct {
	grpc.ClientStream
}

func (x *partitionAssignmentServiceAssignPartitionsClient) Send(m *PartitionAssignmentRequest) error {
	return x.ClientStream.SendMsg(m)
}

func (x *partitionAssignmentServiceAssignPartitionsClient) Recv() (*PartitionAssignment, error) {
	m := new(PartitionAssignment)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// PartitionAssignmentServiceServer is the server API for PartitionAssignmentService service.
// All implementations must embed UnimplementedPartitionAssignmentServiceServer
// for forward compatibility
type PartitionAssignmentServiceServer interface {
	// Assign partitions for this client to handle for the specified subscription.
	//
	// The client must send an InitialPartitionAssignmentRequest first.
	// The server will then send at most one unacknowledged PartitionAssignment
	// outstanding on the stream at a time.
	// The client should send a PartitionAssignmentAck after updating the
	// partitions it is connected to to reflect the new assignment.
	AssignPartitions(PartitionAssignmentService_AssignPartitionsServer) error
	mustEmbedUnimplementedPartitionAssignmentServiceServer()
}

// UnimplementedPartitionAssignmentServiceServer must be embedded to have forward compatible implementations.
type UnimplementedPartitionAssignmentServiceServer struct {
}

func (UnimplementedPartitionAssignmentServiceServer) AssignPartitions(PartitionAssignmentService_AssignPartitionsServer) error {
	return status.Errorf(codes.Unimplemented, "method AssignPartitions not implemented")
}
func (UnimplementedPartitionAssignmentServiceServer) mustEmbedUnimplementedPartitionAssignmentServiceServer() {
}

// UnsafePartitionAssignmentServiceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to PartitionAssignmentServiceServer will
// result in compilation errors.
type UnsafePartitionAssignmentServiceServer interface {
	mustEmbedUnimplementedPartitionAssignmentServiceServer()
}

func RegisterPartitionAssignmentServiceServer(s grpc.ServiceRegistrar, srv PartitionAssignmentServiceServer) {
	s.RegisterService(&PartitionAssignmentService_ServiceDesc, srv)
}

func _PartitionAssignmentService_AssignPartitions_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(PartitionAssignmentServiceServer).AssignPartitions(&partitionAssignmentServiceAssignPartitionsServer{stream})
}

type PartitionAssignmentService_AssignPartitionsServer interface {
	Send(*PartitionAssignment) error
	Recv() (*PartitionAssignmentRequest, error)
	grpc.ServerStream
}

type partitionAssignmentServiceAssignPartitionsServer struct {
	grpc.ServerStream
}

func (x *partitionAssignmentServiceAssignPartitionsServer) Send(m *PartitionAssignment) error {
	return x.ServerStream.SendMsg(m)
}

func (x *partitionAssignmentServiceAssignPartitionsServer) Recv() (*PartitionAssignmentRequest, error) {
	m := new(PartitionAssignmentRequest)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// PartitionAssignmentService_ServiceDesc is the grpc.ServiceDesc for PartitionAssignmentService service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var PartitionAssignmentService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "mockgcp.cloud.pubsublite.v1.PartitionAssignmentService",
	HandlerType: (*PartitionAssignmentServiceServer)(nil),
	Methods:     []grpc.MethodDesc{},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "AssignPartitions",
			Handler:       _PartitionAssignmentService_AssignPartitions_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
	},
	Metadata: "mockgcp/cloud/pubsublite/v1/subscriber.proto",
}