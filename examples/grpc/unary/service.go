package unary

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const Method = "/faultline.demo.Echo/Call"

type Service interface {
	Call(context.Context, *wrapperspb.BytesValue) (*wrapperspb.BytesValue, error)
}

type Echo struct{}

func (Echo) Call(ctx context.Context, request *wrapperspb.BytesValue) (*wrapperspb.BytesValue, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	header := metadata.Pairs("demo-header", "received")
	for _, name := range []string{"demo-metadata", "demo-bin"} {
		if values := md.Get(name); len(values) > 0 {
			header.Set(name, values...)
		}
	}
	grpc.SendHeader(ctx, header)
	grpc.SetTrailer(ctx, metadata.Pairs("demo-trailer", "finished"))
	if values := md.Get("demo-error"); len(values) > 0 && values[0] == "true" {
		return nil, status.Error(codes.FailedPrecondition, "requested demo error")
	}
	return request, nil
}

func Register(server *grpc.Server, service Service) {
	server.RegisterService(&grpc.ServiceDesc{
		ServiceName: "faultline.demo.Echo", HandlerType: (*Service)(nil),
		Methods: []grpc.MethodDesc{{MethodName: "Call", Handler: func(srv any, ctx context.Context, decode func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
			request := new(wrapperspb.BytesValue)
			if err := decode(request); err != nil {
				return nil, err
			}
			if interceptor == nil {
				return srv.(Service).Call(ctx, request)
			}
			return interceptor(ctx, request, &grpc.UnaryServerInfo{Server: srv, FullMethod: Method}, func(ctx context.Context, r any) (any, error) {
				return srv.(Service).Call(ctx, r.(*wrapperspb.BytesValue))
			})
		}}},
	}, service)
}
