package gqlgenx

import "testing"

func TestScan(t *testing.T) {
	doc, err := Scan("testdata")
	if err != nil {
		t.Fatal(err)
	}

	if len(doc.Queries) != 2 {
		t.Fatalf("queries = %+v", doc.Queries)
	}
	userField, usersField := doc.Queries[0], doc.Queries[1]
	if userField.Name == "Users" {
		userField, usersField = usersField, userField
	}
	// User(id string) (*User, error): the value arg is non-null (string!), the
	// pointer result nullable (User).
	if userField.Name != "User" || userField.ReturnType != "User" {
		t.Errorf("User query = %+v", userField)
	}
	if len(userField.Args) != 1 || userField.Args[0].Name != "id" || userField.Args[0].Type != "string!" {
		t.Errorf("User args = %+v", userField.Args)
	}
	// Users(role Role) ([]*User, error): a non-null list of nullable users, and
	// the enum arg carries through non-null.
	if usersField.Name != "Users" || usersField.ReturnType != "[User]!" {
		t.Errorf("Users query = %+v", usersField)
	}
	if len(usersField.Args) != 1 || usersField.Args[0].Type != "Role!" {
		t.Errorf("Users args = %+v", usersField.Args)
	}

	if len(doc.Mutations) != 1 || doc.Mutations[0].Name != "CreateUser" {
		t.Fatalf("mutations = %+v", doc.Mutations)
	}
	// input NewUser is a value, so the argument is non-null.
	if doc.Mutations[0].ReturnType != "User" {
		t.Errorf("CreateUser return = %+v", doc.Mutations[0])
	}
	if doc.Mutations[0].Args[0].Type != "NewUser!" {
		t.Errorf("CreateUser args = %+v", doc.Mutations[0].Args)
	}

	if doc.Types["User"] == nil {
		t.Errorf("User type missing: %+v", doc.Types)
	}
	if doc.Types["NewUser"] == nil {
		t.Errorf("NewUser type missing: %+v", doc.Types)
	}
	// The Role enum surfaces with its values rather than as a bare Go type name.
	role := doc.Types["Role"]
	if role == nil {
		t.Fatalf("Role enum missing: %+v", doc.Types)
	}
	if role.Type != "string" || len(role.Enum) != 2 {
		t.Errorf("Role enum = %+v, want string with 2 values", role)
	}
}
