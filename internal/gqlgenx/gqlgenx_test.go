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
	if userField.Name != "User" || userField.ReturnType != "User" {
		t.Errorf("User query = %+v", userField)
	}
	if len(userField.Args) != 1 || userField.Args[0].Name != "id" || userField.Args[0].Type != "string" {
		t.Errorf("User args = %+v", userField.Args)
	}
	if usersField.Name != "Users" || usersField.ReturnType != "[User]" {
		t.Errorf("Users query = %+v", usersField)
	}

	if len(doc.Mutations) != 1 || doc.Mutations[0].Name != "CreateUser" {
		t.Fatalf("mutations = %+v", doc.Mutations)
	}
	if doc.Mutations[0].ReturnType != "User" {
		t.Errorf("CreateUser return = %+v", doc.Mutations[0])
	}

	if doc.Types["User"] == nil {
		t.Errorf("User type missing: %+v", doc.Types)
	}
	if doc.Types["NewUser"] == nil {
		t.Errorf("NewUser type missing: %+v", doc.Types)
	}
}
