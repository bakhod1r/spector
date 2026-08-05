package testdata

type Role string

const (
	RoleAdmin Role = "ADMIN"
	RoleUser  Role = "USER"
)

type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type NewUser struct {
	Name string `json:"name"`
}
