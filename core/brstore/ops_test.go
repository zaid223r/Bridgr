package brstore_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/zaid223r/Bridgr/core/brschema"
	"github.com/zaid223r/Bridgr/core/brstore"
)

func TestOpsFor(t *testing.T) {
	t.Parallel()
	type all struct {
		S  string    `json:"s"`
		PS *string   `json:"ps"`
		B  bool      `json:"b"`
		I  int       `json:"i"`
		U  uint8     `json:"u"`
		F  float64   `json:"f"`
		T  time.Time `json:"t"`
		Bs []byte    `json:"bs"`
	}
	m, err := brschema.Of[all]()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"s":  "[eq ne contains in]",
		"ps": "[eq ne contains in]",
		"b":  "[eq ne]",
		"i":  "[eq ne gt gte lt lte in]",
		"u":  "[eq ne gt gte lt lte in]",
		"f":  "[eq ne gt gte lt lte in]",
		"t":  "[eq ne gt gte lt lte in]",
		"bs": "[]",
	}
	for name, w := range want {
		f, _ := m.FieldByJSON(name)
		if got := fmt.Sprint(brstore.OpsFor(f)); got != w {
			t.Errorf("OpsFor(%s) = %s, want %s", name, got, w)
		}
	}
}

func TestParseOp(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"eq", "ne", "gt", "gte", "lt", "lte", "contains", "in"} {
		if op, ok := brstore.ParseOp(s); !ok || string(op) != s {
			t.Errorf("ParseOp(%q) = %q, %v", s, op, ok)
		}
	}
	for _, s := range []string{"", "EQ", "like", "between", "gte;", "in "} {
		if _, ok := brstore.ParseOp(s); ok {
			t.Errorf("ParseOp(%q) accepted", s)
		}
	}
}
