package testdata

import "context"

type QueryResolver interface {
	User(ctx context.Context, id string) (*User, error)
	Users(ctx context.Context) ([]*User, error)
}

type MutationResolver interface {
	CreateUser(ctx context.Context, input NewUser) (*User, error)
}
