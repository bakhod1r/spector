package main

import (
	"context"
	"log"
	"net"
	"strings"

	"github.com/user/specter/examples/shop/shoppb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

type userServer struct {
	shoppb.UnimplementedUserServiceServer
}

var seedUsers = []*shoppb.User{
	{Id: 1, Name: "Ada", Email: "ada@example.com", Roles: []string{"admin"}},
	{Id: 2, Name: "Alan", Email: "alan@example.com", Roles: []string{"user"}},
	{Id: 3, Name: "Grace", Email: "grace@example.com", Roles: []string{"user", "editor"}},
}

func (s *userServer) GetUser(ctx context.Context, req *shoppb.GetUserRequest) (*shoppb.User, error) {
	for _, u := range seedUsers {
		if u.Id == req.Id {
			return u, nil
		}
	}
	return &shoppb.User{}, nil
}

func (s *userServer) ListUsers(ctx context.Context, req *shoppb.ListUsersRequest) (*shoppb.ListUsersResponse, error) {
	out := []*shoppb.User{}
	for _, u := range seedUsers {
		if req.Q != "" && !strings.Contains(strings.ToLower(u.Name), strings.ToLower(req.Q)) {
			continue
		}
		out = append(out, u)
	}
	return &shoppb.ListUsersResponse{Users: out, Total: int32(len(out))}, nil
}

func (s *userServer) CreateUser(ctx context.Context, req *shoppb.CreateUserRequest) (*shoppb.User, error) {
	return &shoppb.User{Id: int32(len(seedUsers) + 1), Name: req.Name, Email: req.Email, Roles: req.Roles}, nil
}

func (s *userServer) DeleteUser(ctx context.Context, req *shoppb.DeleteUserRequest) (*shoppb.Empty, error) {
	return &shoppb.Empty{}, nil
}

func (s *userServer) StreamUsers(req *shoppb.ListUsersRequest, stream grpc.ServerStreamingServer[shoppb.User]) error {
	for _, u := range seedUsers {
		if err := stream.Send(u); err != nil {
			return err
		}
	}
	return nil
}

func startGRPC(addr string) {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("grpc listen: %v", err)
		return
	}
	s := grpc.NewServer()
	shoppb.RegisterUserServiceServer(s, &userServer{})
	reflection.Register(s)
	log.Printf("gRPC server on %s", addr)
	if err := s.Serve(lis); err != nil {
		log.Printf("grpc serve: %v", err)
	}
}
