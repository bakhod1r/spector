package core

import (
	"go/parser"
	"go/token"
	"testing"
)

const enumSrc = `package sample

type Status string

const (
	StatusActive   Status = "active"
	StatusInactive Status = "inactive"
	StatusBanned   Status = "banned"
)

type Priority int

const (
	PriorityLow  Priority = 1
	PriorityHigh Priority = 5
)

type Color int

const (
	Red Color = iota
	Green
	Blue
)

type Flag uint

const (
	FlagA Flag = 1 << iota
	FlagB
	FlagC
)

type Level int

const (
	LevelLow Level = iota + 10
	LevelMid
	LevelHigh
)

type User struct {
	Name   string ` + "`json:\"name\"`" + `
	Status Status ` + "`json:\"status\"`" + `
}
`

func TestCollectEnums(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", enumSrc, 0)
	if err != nil {
		t.Fatal(err)
	}

	s := NewStructScanner()
	s.Collect(file)

	status := s.Schemas["Status"]
	if status == nil {
		t.Fatal("Status enum not collected")
	}
	if status.Type != "string" {
		t.Errorf("Status type = %q, want string", status.Type)
	}
	if got, want := len(status.Enum), 3; got != want {
		t.Fatalf("Status enum len = %d, want %d (%v)", got, want, status.Enum)
	}
	if status.Enum[0] != "active" || status.Enum[2] != "banned" {
		t.Errorf("Status enum values = %v", status.Enum)
	}

	prio := s.Schemas["Priority"]
	if prio == nil || prio.Type != "integer" {
		t.Fatalf("Priority enum wrong: %+v", prio)
	}
	if len(prio.Enum) != 2 || prio.Enum[0] != 1 || prio.Enum[1] != 5 {
		t.Errorf("Priority enum values = %v", prio.Enum)
	}

	// iota: 0, 1, 2
	if got := s.Schemas["Color"]; got == nil || len(got.Enum) != 3 ||
		got.Enum[0] != 0 || got.Enum[1] != 1 || got.Enum[2] != 2 {
		t.Errorf("Color enum = %+v", got)
	}
	// 1 << iota: 1, 2, 4
	if got := s.Schemas["Flag"]; got == nil || len(got.Enum) != 3 ||
		got.Enum[0] != 1 || got.Enum[1] != 2 || got.Enum[2] != 4 {
		t.Errorf("Flag enum = %+v", got)
	}
	// iota + 10: 10, 11, 12
	if got := s.Schemas["Level"]; got == nil || len(got.Enum) != 3 ||
		got.Enum[0] != 10 || got.Enum[1] != 11 || got.Enum[2] != 12 {
		t.Errorf("Level enum = %+v", got)
	}

	// The struct field should $ref the enum type so the values are documented.
	user := s.Schemas["User"]
	if user == nil {
		t.Fatal("User struct not collected")
	}
	if ref := user.Properties["status"].Ref; ref != "#/components/schemas/Status" {
		t.Errorf("User.status ref = %q", ref)
	}
}
