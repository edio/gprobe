package acctest

import (
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	hv1 "google.golang.org/grpc/health/grpc_health_v1"
	"net"
)

// StartServer starts new gRPC application with simple health service.
// It is callers responsibility to Stop the server
func StartServer(port int, certFile string, keyFile string) (*grpc.Server, *health.Server, error) {
	transportCredentials, err := credentials.NewServerTLSFromFile(certFile, keyFile)
	if err != nil {
		return nil, nil, err
	}
	return doStart(port, grpc.Creds(transportCredentials))
}

// StartInsecureServer starts new gRPC application with simple health service.
// It is callers responsibility to Stop the server
func StartInsecureServer(port int) (*grpc.Server, *health.Server, error) {
	return doStart(port)
}

func doStart(port int, options ...grpc.ServerOption) (server *grpc.Server, service *health.Server, err error) {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return
	}
	server = grpc.NewServer(options...)
	service = health.NewServer()
	hv1.RegisterHealthServer(server, service)

	go server.Serve(listener)
	return server, service, nil
}
