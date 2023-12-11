// Code generated by protoc-gen-grpc-gateway. DO NOT EDIT.
// source: opencensus/proto/agent/trace/v1/trace_service.proto

/*
Package v1 is a reverse proxy.

It translates gRPC into RESTful JSON APIs.
*/
package v1

import (
	"context"
	"io"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/grpc-ecosystem/grpc-gateway/v2/utilities"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Suppress "imported and not used" errors
var _ codes.Code
var _ io.Reader
var _ status.Status
var _ = runtime.String
var _ = utilities.NewDoubleArray
var _ = metadata.Join

func request_TraceService_Export_0(ctx context.Context, marshaler runtime.Marshaler, client TraceServiceClient, req *http.Request, pathParams map[string]string) (TraceService_ExportClient, runtime.ServerMetadata, error) {
	var metadata runtime.ServerMetadata
	stream, err := client.Export(ctx)
	if err != nil {
		grpclog.Infof("Failed to start streaming: %v", err)
		return nil, metadata, err
	}
	dec := marshaler.NewDecoder(req.Body)
	handleSend := func() error {
		var protoReq ExportTraceServiceRequest
		err := dec.Decode(&protoReq)
		if err == io.EOF {
			return err
		}
		if err != nil {
			grpclog.Infof("Failed to decode request: %v", err)
			return err
		}
		if err := stream.Send(&protoReq); err != nil {
			grpclog.Infof("Failed to send request: %v", err)
			return err
		}
		return nil
	}
	go func() {
		for {
			if err := handleSend(); err != nil {
				break
			}
		}
		if err := stream.CloseSend(); err != nil {
			grpclog.Infof("Failed to terminate client stream: %v", err)
		}
	}()
	header, err := stream.Header()
	if err != nil {
		grpclog.Infof("Failed to get header from client: %v", err)
		return nil, metadata, err
	}
	metadata.HeaderMD = header
	return stream, metadata, nil
}

// RegisterTraceServiceHandlerServer registers the http handlers for service TraceService to "mux".
// UnaryRPC     :call TraceServiceServer directly.
// StreamingRPC :currently unsupported pending https://github.com/grpc/grpc-go/issues/906.
// Note that using this registration option will cause many gRPC library features to stop working. Consider using RegisterTraceServiceHandlerFromEndpoint instead.
func RegisterTraceServiceHandlerServer(ctx context.Context, mux *runtime.ServeMux, server TraceServiceServer) error {

	mux.Handle("POST", pattern_TraceService_Export_0, func(w http.ResponseWriter, req *http.Request, pathParams map[string]string) {
		err := status.Error(codes.Unimplemented, "streaming calls are not yet supported in the in-process transport")
		_, outboundMarshaler := runtime.MarshalerForRequest(mux, req)
		runtime.HTTPError(ctx, mux, outboundMarshaler, w, req, err)
		return
	})

	return nil
}

// RegisterTraceServiceHandlerFromEndpoint is same as RegisterTraceServiceHandler but
// automatically dials to "endpoint" and closes the connection when "ctx" gets done.
func RegisterTraceServiceHandlerFromEndpoint(ctx context.Context, mux *runtime.ServeMux, endpoint string, opts []grpc.DialOption) (err error) {
	conn, err := grpc.Dial(endpoint, opts...)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if cerr := conn.Close(); cerr != nil {
				grpclog.Infof("Failed to close conn to %s: %v", endpoint, cerr)
			}
			return
		}
		go func() {
			<-ctx.Done()
			if cerr := conn.Close(); cerr != nil {
				grpclog.Infof("Failed to close conn to %s: %v", endpoint, cerr)
			}
		}()
	}()

	return RegisterTraceServiceHandler(ctx, mux, conn)
}

// RegisterTraceServiceHandler registers the http handlers for service TraceService to "mux".
// The handlers forward requests to the grpc endpoint over "conn".
func RegisterTraceServiceHandler(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error {
	return RegisterTraceServiceHandlerClient(ctx, mux, NewTraceServiceClient(conn))
}

// RegisterTraceServiceHandlerClient registers the http handlers for service TraceService
// to "mux". The handlers forward requests to the grpc endpoint over the given implementation of "TraceServiceClient".
// Note: the gRPC framework executes interceptors within the gRPC handler. If the passed in "TraceServiceClient"
// doesn't go through the normal gRPC flow (creating a gRPC client etc.) then it will be up to the passed in
// "TraceServiceClient" to call the correct interceptors.
func RegisterTraceServiceHandlerClient(ctx context.Context, mux *runtime.ServeMux, client TraceServiceClient) error {

	mux.Handle("POST", pattern_TraceService_Export_0, func(w http.ResponseWriter, req *http.Request, pathParams map[string]string) {
		ctx, cancel := context.WithCancel(req.Context())
		defer cancel()
		inboundMarshaler, outboundMarshaler := runtime.MarshalerForRequest(mux, req)
		rctx, err := runtime.AnnotateContext(ctx, mux, req, "/opencensus.proto.agent.trace.v1.TraceService/Export", runtime.WithHTTPPathPattern("/v1/trace"))
		if err != nil {
			runtime.HTTPError(ctx, mux, outboundMarshaler, w, req, err)
			return
		}
		resp, md, err := request_TraceService_Export_0(rctx, inboundMarshaler, client, req, pathParams)
		ctx = runtime.NewServerMetadataContext(ctx, md)
		if err != nil {
			runtime.HTTPError(ctx, mux, outboundMarshaler, w, req, err)
			return
		}

		forward_TraceService_Export_0(ctx, mux, outboundMarshaler, w, req, func() (proto.Message, error) { return resp.Recv() }, mux.GetForwardResponseOptions()...)

	})

	return nil
}

var (
	pattern_TraceService_Export_0 = runtime.MustPattern(runtime.NewPattern(1, []int{2, 0, 2, 1}, []string{"v1", "trace"}, ""))
)

var (
	forward_TraceService_Export_0 = runtime.ForwardResponseStream
)