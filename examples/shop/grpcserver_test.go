package main

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/user/specter/examples/shop/shoppb"
)

func TestGetUserFindsSeededUser(t *testing.T) {
	got, err := (&userServer{}).GetUser(context.Background(), &shoppb.GetUserRequest{Id: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Ada" {
		t.Errorf("name = %q, want Ada", got.Name)
	}
}

// An unknown id yields an empty user rather than an error, which is what the
// console's Execute button shows.
func TestGetUserUnknownIDReturnsEmpty(t *testing.T) {
	got, err := (&userServer{}).GetUser(context.Background(), &shoppb.GetUserRequest{Id: 999})
	if err != nil {
		t.Fatal(err)
	}
	if got.Id != 0 || got.Name != "" {
		t.Errorf("= %+v, want an empty user", got)
	}
}

func TestListUsersReturnsAll(t *testing.T) {
	got, err := (&userServer{}).ListUsers(context.Background(), &shoppb.ListUsersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Users) != len(seedUsers) {
		t.Errorf("users = %d, want %d", len(got.Users), len(seedUsers))
	}
	if int(got.Total) != len(got.Users) {
		t.Errorf("total = %d, want %d", got.Total, len(got.Users))
	}
}

// The query filter is case-insensitive and matches on a substring.
func TestListUsersFiltersByQuery(t *testing.T) {
	s := &userServer{}
	cases := []struct {
		q    string
		want int
	}{
		{"ada", 1},
		{"ADA", 1},
		{"a", 3},
		{"nobody", 0},
	}
	for _, tc := range cases {
		t.Run(tc.q, func(t *testing.T) {
			got, err := s.ListUsers(context.Background(), &shoppb.ListUsersRequest{Q: tc.q})
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Users) != tc.want {
				t.Errorf("users = %d, want %d", len(got.Users), tc.want)
			}
		})
	}
}

func TestCreateUserEchoesInput(t *testing.T) {
	got, err := (&userServer{}).CreateUser(context.Background(), &shoppb.CreateUserRequest{
		Name:  "Grace",
		Email: "grace@example.com",
		Roles: []string{"admin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Grace" || got.Email != "grace@example.com" {
		t.Errorf("= %+v, want the input echoed", got)
	}
	if len(got.Roles) != 1 || got.Roles[0] != "admin" {
		t.Errorf("roles = %v", got.Roles)
	}
	if got.Id == 0 {
		t.Error("no id assigned")
	}
}

func TestDeleteUser(t *testing.T) {
	if _, err := (&userServer{}).DeleteUser(context.Background(), &shoppb.DeleteUserRequest{Id: 1}); err != nil {
		t.Fatal(err)
	}
}

// fakeStream records what StreamUsers sends, and can be told to fail so the
// error path is exercised too.
type fakeStream struct {
	grpc.ServerStream
	sent    []*shoppb.User
	failAt  int
	sendErr error
}

func (f *fakeStream) Send(u *shoppb.User) error {
	if f.sendErr != nil && len(f.sent) == f.failAt {
		return f.sendErr
	}
	f.sent = append(f.sent, u)
	return nil
}

func (f *fakeStream) Context() context.Context { return context.Background() }

func TestStreamUsersSendsEveryone(t *testing.T) {
	stream := &fakeStream{}
	if err := (&userServer{}).StreamUsers(&shoppb.ListUsersRequest{}, stream); err != nil {
		t.Fatal(err)
	}
	if len(stream.sent) != len(seedUsers) {
		t.Errorf("sent = %d, want %d", len(stream.sent), len(seedUsers))
	}
}

// A send failure aborts the stream instead of being swallowed.
func TestStreamUsersPropagatesSendError(t *testing.T) {
	want := errors.New("connection reset")
	stream := &fakeStream{failAt: 1, sendErr: want}

	err := (&userServer{}).StreamUsers(&shoppb.ListUsersRequest{}, stream)
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	if len(stream.sent) != 1 {
		t.Errorf("sent = %d, want the stream to stop at the failure", len(stream.sent))
	}
}

// startGRPC must serve on the address it is given, and must return rather than
// block when the port is already taken.
func TestStartGRPCServes(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := lis.Addr().String()
	lis.Close()

	go startGRPC(addr)

	cc, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Close()

	client := shoppb.NewUserServiceClient(cc)
	got, err := client.GetUser(context.Background(), &shoppb.GetUserRequest{Id: 1})
	if err != nil {
		t.Fatalf("GetUser over the wire: %v", err)
	}
	if got.Name != "Ada" {
		t.Errorf("name = %q, want Ada", got.Name)
	}
}

// A port already in use is logged and returns; it must not panic or block.
func TestStartGRPCBusyPortReturns(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer lis.Close()

	done := make(chan struct{})
	go func() {
		startGRPC(lis.Addr().String())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("startGRPC did not return on a busy port")
	}
}
