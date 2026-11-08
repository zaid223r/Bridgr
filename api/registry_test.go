package api

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/zaid223r/Bridgr/core/brschema"
)

var update = flag.Bool("update", false, "rewrite golden files")

type User struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

type Todo struct {
	ID         int64             `json:"id"`
	Title      string            `json:"title"`
	Done       bool              `json:"done"`
	Priority   uint8             `json:"priority"`
	Due        *time.Time        `json:"due,omitempty"`
	Tags       []string          `json:"tags"`
	Meta       map[string]string `json:"meta"`
	Owner      User              `json:"owner"`
	Reviewer   *User             `json:"reviewer"`
	Attachment json.RawMessage   `json:"attachment"`
	Secret     string            `json:"-"`
}

func (Todo) FilterableFields() []string { return []string{"title", "done", "priority", "due"} }

type OrderItem struct {
	SKU string  `json:"sku" db:",pk"`
	Qty int     `json:"qty"`
	Net float64 `json:"net"`
}

func (OrderItem) GenericFiltering() bool { return true }

func mustModel[T any](t *testing.T) *brschema.Model {
	t.Helper()
	m, err := brschema.Of[T]()
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func sampleRegistry(t *testing.T) *Registry {
	t.Helper()
	r := NewRegistry()
	r.AddCRUD("todos", mustModel[Todo](t), CRUDDocs{Auth: true, Validate: true, DefaultLimit: 20, MaxLimit: 100})
	r.AddCRUD("order-items", mustModel[OrderItem](t), CRUDDocs{DefaultLimit: 50, MaxLimit: 200})
	return r
}

// TestGolden compares the whole document with testdata/crud.golden.json. Run
// `go test ./api -update` after an intended change and review the diff.
func TestGolden(t *testing.T) {
	t.Parallel()
	got, err := sampleRegistry(t).Build(Info{Title: "Shop", Version: "2.1.0", Description: "Golden test"})
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "crud.golden.json")
	if *update {
		if err := os.WriteFile(golden, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run `go test ./api -update` to create it)", err)
	}
	if !bytes.Equal(append(got, '\n'), want) {
		t.Errorf("document differs from %s; run `go test ./api -update` and review the diff", golden)
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	t.Parallel()
	first, err := sampleRegistry(t).Build(Info{})
	if err != nil {
		t.Fatal(err)
	}
	for range 20 {
		again, _ := sampleRegistry(t).Build(Info{})
		if !bytes.Equal(first, again) {
			t.Fatal("two builds of the same registry differ")
		}
	}
}

type operation struct {
	OperationID string                     `json:"operationId"`
	Responses   map[string]json.RawMessage `json:"responses"`
}

type doc struct {
	Info       map[string]string                     `json:"info"`
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components struct {
		Schemas map[string]json.RawMessage `json:"schemas"`
	} `json:"components"`
}

func build(t *testing.T, r *Registry) doc {
	t.Helper()
	raw, err := r.Build(Info{})
	if err != nil {
		t.Fatal(err)
	}
	var d doc
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestOperationIDsAndResponses(t *testing.T) {
	t.Parallel()
	d := build(t, sampleRegistry(t))

	if d.Info["title"] != "Bridgr API" || d.Info["version"] != "1.0.0" {
		t.Errorf("default info = %v", d.Info)
	}

	want := map[string]map[string]struct {
		id    string
		codes []string
	}{
		"/todos": {
			"get":  {"listTodos", []string{"200", "400", "401", "403", "500"}},
			"post": {"createTodo", []string{"201", "400", "401", "403", "409", "413", "422", "500"}},
		},
		"/todos/{id}": {
			"get":    {"getTodo", []string{"200", "401", "403", "404", "500"}},
			"put":    {"replaceTodo", []string{"200", "400", "401", "403", "404", "409", "413", "422", "500"}},
			"patch":  {"updateTodo", []string{"200", "400", "401", "403", "404", "409", "413", "422", "500"}},
			"delete": {"deleteTodo", []string{"204", "401", "403", "404", "500"}},
		},
		"/order-items": {
			"get":  {"listOrderItems", []string{"200", "400", "500"}},
			"post": {"createOrderItem", []string{"201", "400", "409", "413", "500"}},
		},
		"/order-items/{id}": {
			"delete": {"deleteOrderItem", []string{"204", "404", "500"}},
		},
	}
	for path, ops := range want {
		for method, w := range ops {
			var op operation
			if err := json.Unmarshal(d.Paths[path][method], &op); err != nil {
				t.Fatalf("%s %s: %v", method, path, err)
			}
			if op.OperationID != w.id {
				t.Errorf("%s %s operationId = %q, want %q", method, path, op.OperationID, w.id)
			}
			var codes []string
			for c := range op.Responses {
				codes = append(codes, c)
			}
			slices.Sort(codes)
			if !slices.Equal(codes, w.codes) {
				t.Errorf("%s %s responses = %v, want %v", method, path, codes, w.codes)
			}
		}
	}

	var schemas []string
	for name := range d.Components.Schemas {
		schemas = append(schemas, name)
	}
	slices.Sort(schemas)
	if want := []string{"OrderItem", "OrderItemInput", "Problem", "Todo", "TodoInput", "User"}; !slices.Equal(schemas, want) {
		t.Errorf("component schemas = %v, want %v", schemas, want)
	}
}

func TestInputSchemas(t *testing.T) {
	t.Parallel()
	d := build(t, sampleRegistry(t))
	var todoIn, skuIn, todo struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	_ = json.Unmarshal(d.Components.Schemas["TodoInput"], &todoIn)
	_ = json.Unmarshal(d.Components.Schemas["OrderItemInput"], &skuIn)
	_ = json.Unmarshal(d.Components.Schemas["Todo"], &todo)

	if _, ok := todoIn.Properties["id"]; ok || len(todoIn.Required) != 0 {
		t.Errorf("TodoInput should drop the generated id and require nothing: %+v", todoIn)
	}
	if _, ok := skuIn.Properties["sku"]; !ok {
		t.Error("OrderItemInput must keep the client-chosen string key")
	}
	if want := []string{"id", "title", "done", "priority", "tags", "meta", "owner", "reviewer", "attachment"}; !slices.Equal(todo.Required, want) {
		t.Errorf("Todo required = %v, want %v (omitempty fields excluded)", todo.Required, want)
	}
	if _, ok := todo.Properties["secret"]; ok {
		t.Error(`json:"-" field documented`)
	}
}

func TestSchemaFor(t *testing.T) {
	t.Parallel()
	type anon = struct {
		A int `json:"a"`
	}
	type Recursive struct {
		Children []Recursive `json:"children"`
	}
	tests := []struct {
		name string
		typ  any
		want string
	}{
		{"bool", true, `{"type":"boolean"}`},
		{"int16", int16(0), `{"format":"int32","type":"integer"}`},
		{"int", 0, `{"format":"int64","type":"integer"}`},
		{"uint32", uint32(0), `{"format":"int32","minimum":0,"type":"integer"}`},
		{"uint64", uint64(0), `{"format":"int64","minimum":0,"type":"integer"}`},
		{"float32", float32(0), `{"format":"float","type":"number"}`},
		{"float64", 0.0, `{"format":"double","type":"number"}`},
		{"string", "", `{"type":"string"}`},
		{"time", time.Time{}, `{"format":"date-time","type":"string"}`},
		{"pointer", new(string), `{"type":["string","null"]}`},
		{"bytes", []byte{}, `{"contentEncoding":"base64","type":["string","null"]}`},
		{"slice", []int{}, `{"items":{"format":"int64","type":"integer"},"type":["array","null"]}`},
		{"array", [2]bool{}, `{"items":{"type":"boolean"},"maxItems":2,"minItems":2,"type":"array"}`},
		{"map", map[string]float64{}, `{"additionalProperties":{"format":"double","type":"number"},"type":["object","null"]}`},
		{"raw json", json.RawMessage{}, `{}`},
		{"any", new(any), `{}`},
		{"anonymous struct", anon{}, `{"properties":{"a":{"format":"int64","type":"integer"}},"required":["a"],"type":"object"}`},
		{"named struct", User{}, `{"$ref":"#/components/schemas/User"}`},
		{"pointer to named struct", &User{}, `{"anyOf":[{"$ref":"#/components/schemas/User"},{"type":"null"}]}`},
		{"recursive struct", Recursive{}, `{"$ref":"#/components/schemas/Recursive"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := newSchemaGen()
			typ := reflectType(tt.typ)
			got, _ := json.Marshal(g.schemaFor(typ))
			if string(got) != tt.want {
				t.Errorf("schemaFor(%s) = %s, want %s", typ, got, tt.want)
			}
		})
	}
}

func TestComponentNames(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"Todo":                       "Todo",
		"Page[example.com/app.Todo]": "PageTodo",
		"Pair[int,example.com/x.Y]":  "PairIntY",
		"order_items":                "OrderItems",
	}
	for in, want := range tests {
		if got := typeName(in); got != want {
			t.Errorf("typeName(%q) = %q, want %q", in, got, want)
		}
	}

	g := newSchemaGen()
	type User struct {
		ID int `json:"id"`
	}
	g.ref(reflectType(User{}))
	g.ref(reflectType(User{})) // same type: same name
	if _, ok := g.components["User"]; !ok {
		t.Fatal("first User not named User")
	}
	other := g.uniqueName(reflectType(struct{ X int }{}))
	if other == "User" {
		t.Error("different type got a taken name")
	}
	if g.uniqueName(reflectType(User{})) != "User" {
		t.Error("same type should keep its name")
	}
}
