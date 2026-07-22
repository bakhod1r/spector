package graphqlsdl

import "testing"

func TestScan(t *testing.T) {
	doc, err := Scan("testdata")
	if err != nil {
		t.Fatal(err)
	}

	if len(doc.Queries) != 2 {
		t.Fatalf("queries = %+v", doc.Queries)
	}
	userField := doc.Queries[0]
	if userField.Name != "user" || userField.ReturnType != "User" {
		t.Errorf("user query = %+v", userField)
	}
	if len(userField.Args) != 1 || userField.Args[0].Name != "id" || userField.Args[0].Type != "ID!" {
		t.Errorf("user args = %+v", userField.Args)
	}
	if userField.Description != "Fetch a single user by id" {
		t.Errorf("description = %q", userField.Description)
	}

	if len(doc.Mutations) != 1 || doc.Mutations[0].Name != "createUser" {
		t.Fatalf("mutations = %+v", doc.Mutations)
	}

	u := doc.Types["User"]
	if u == nil {
		t.Fatal("User type missing")
	}
	if roles := u.Properties["roles"]; roles == nil || roles.Type != "array" {
		t.Errorf("roles = %+v", roles)
	}

	if _, ok := doc.Types["NewUser"]; !ok {
		t.Error("NewUser input type missing")
	}

	status := doc.Enums["Status"]
	if status == nil || len(status.Enum) != 3 || status.Enum[0] != "STATUS_UNKNOWN" {
		t.Errorf("Status enum = %+v", status)
	}
}
