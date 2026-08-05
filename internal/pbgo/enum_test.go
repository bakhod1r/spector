package pbgo

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/user/specter/internal/core"
)

// A generated .pb.go enum surfaces its symbolic names, not the raw int32, and
// they come out in wire-number order even when the map literal is unordered.
const enumSrc = `package shoppb

type OrderStatus int32

const (
	OrderStatus_ORDER_STATUS_UNSPECIFIED OrderStatus = 0
	OrderStatus_PENDING                  OrderStatus = 1
	OrderStatus_SHIPPED                  OrderStatus = 2
)

var OrderStatus_name = map[int32]string{
	2: "SHIPPED",
	0: "ORDER_STATUS_UNSPECIFIED",
	1: "PENDING",
}

var OrderStatus_value = map[string]int32{
	"ORDER_STATUS_UNSPECIFIED": 0,
	"PENDING":                  1,
	"SHIPPED":                  2,
}

type Order struct {
	Id     string      ` + "`json:\"id\"`" + `
	Status OrderStatus ` + "`json:\"status\"`" + `
}
`

func TestEnumNamesOverrideIntegers(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "shop.pb.go", enumSrc, 0)
	if err != nil {
		t.Fatal(err)
	}

	scanner := core.NewStructScanner()
	scanner.Collect(file)

	// Before the override the scanner sees a bare integer enum.
	before := scanner.Schemas["OrderStatus"]
	if before == nil || before.Type != "integer" {
		t.Fatalf("expected integer enum before override, got %+v", before)
	}

	enums := map[string][]string{}
	collectEnumNames(file, enums)
	if got := enums["OrderStatus"]; len(got) != 3 || got[0] != "ORDER_STATUS_UNSPECIFIED" || got[2] != "SHIPPED" {
		t.Fatalf("enum names = %v, want ordered by wire number", got)
	}

	applyEnumNames(scanner.Schemas, enums)

	s := scanner.Schemas["OrderStatus"]
	if s.Type != "string" {
		t.Errorf("type = %q, want string", s.Type)
	}
	want := []string{"ORDER_STATUS_UNSPECIFIED", "PENDING", "SHIPPED"}
	if len(s.Enum) != len(want) {
		t.Fatalf("enum = %v, want %v", s.Enum, want)
	}
	for i, w := range want {
		if s.Enum[i] != w {
			t.Errorf("enum[%d] = %v, want %q", i, s.Enum[i], w)
		}
	}

	// The referencing field still points at the enum by $ref, so the names now
	// document everywhere the type is used.
	order := scanner.Schemas["Order"]
	if order.Properties["status"].Ref != refPrefix+"OrderStatus" {
		t.Errorf("status field ref = %q", order.Properties["status"].Ref)
	}
}

// A type with no _name map (an ordinary int field) is left untouched.
func TestCollectEnumNamesIgnoresNonEnum(t *testing.T) {
	fset := token.NewFileSet()
	file, _ := parser.ParseFile(fset, "x.pb.go", `package p
var Config_value = map[string]int32{"A": 1}
var Names = map[int32]string{0: "x"}
`, 0)
	enums := map[string][]string{}
	collectEnumNames(file, enums)
	// Config_value is map[string]int32, not the _name map; "Names" has no type
	// suffix. Neither is an enum.
	if len(enums) != 0 {
		t.Errorf("enums = %v, want none", enums)
	}
}
