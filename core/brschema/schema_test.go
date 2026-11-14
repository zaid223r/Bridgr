package brschema_test

import (
	"encoding/json"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zaid223r/Bridgr/core/brschema"
)

type Base struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	CreatedAt time.Time `json:"created_at"`
}

type Audit struct {
	CreatedBy string `json:"created_by"`
}

type Todo struct {
	Base
	*Audit
	Title    string            `json:"title"`
	Done     bool              `json:"done,omitempty"`
	Note     *string           `json:"note"`
	Priority int8              `json:"priority"`
	Score    float64           `json:"score,omitzero"`
	Tags     []string          `json:"tags"`
	Blob     []byte            `json:"blob"`
	Meta     map[string]string `json:"meta"`
	UserID   int64             `json:"user_id"`
	Legacy   string            `json:"legacy" gorm:"column:old_name;size:64"`
	Secret   string            `json:"-"`
	Dash     string            `json:"-,"` //nolint:staticcheck // a field literally named "-" is a real encoding/json edge case
	NoTag    string
	hidden   string //nolint:unused // proves unexported fields are skipped
}

func mustOf[T any](t *testing.T) *brschema.Model {
	t.Helper()
	m, err := brschema.Of[T]()
	if err != nil {
		t.Fatalf("Of: %v", err)
	}
	return m
}

func jsonNames(fields []*brschema.Field) []string {
	names := make([]string, len(fields))
	for i, f := range fields {
		names[i] = f.JSONName
	}
	return names
}

func TestFields(t *testing.T) {
	t.Parallel()
	m := mustOf[Todo](t)

	want := []string{
		"id", "created_at", "created_by", "title", "done", "note", "priority", "score",
		"tags", "blob", "meta", "user_id", "legacy", "-", "NoTag",
	}
	if got := jsonNames(m.Fields); !slices.Equal(got, want) {
		t.Fatalf("fields =\n  %v\nwant\n  %v", got, want)
	}
	if m.Name != "Todo" || m.Type != reflect.TypeFor[Todo]() {
		t.Errorf("Name/Type = %q/%v", m.Name, m.Type)
	}

	tests := []struct {
		json        string
		goName      string
		column      string
		openAPIType string
		format      string
		nullable    bool
		omitEmpty   bool
		index       []int
	}{
		{"id", "ID", "id", "integer", "int64", false, false, []int{0, 0}},
		{"created_at", "CreatedAt", "created_at", "string", "date-time", false, false, []int{0, 1}},
		{"created_by", "CreatedBy", "created_by", "string", "", false, false, []int{1, 0}},
		{"done", "Done", "done", "boolean", "", false, true, []int{3}},
		{"note", "Note", "note", "string", "", true, false, []int{4}},
		{"priority", "Priority", "priority", "integer", "int32", false, false, []int{5}},
		{"score", "Score", "score", "number", "double", false, true, []int{6}},
		{"tags", "Tags", "tags", "array", "", false, false, []int{7}},
		{"blob", "Blob", "blob", "string", "byte", false, false, []int{8}},
		{"meta", "Meta", "meta", "object", "", false, false, []int{9}},
		{"user_id", "UserID", "user_id", "integer", "int64", false, false, []int{10}},
		{"legacy", "Legacy", "old_name", "string", "", false, false, []int{11}},
		{"-", "Dash", "dash", "string", "", false, false, []int{13}},
		{"NoTag", "NoTag", "no_tag", "string", "", false, false, []int{14}},
	}
	for _, tt := range tests {
		t.Run(tt.json, func(t *testing.T) {
			t.Parallel()
			f, ok := m.FieldByJSON(tt.json)
			if !ok {
				t.Fatalf("FieldByJSON(%q) not found", tt.json)
			}
			if f.GoName != tt.goName || f.Column != tt.column {
				t.Errorf("GoName/Column = %s/%s, want %s/%s", f.GoName, f.Column, tt.goName, tt.column)
			}
			if f.OpenAPIType != tt.openAPIType || f.Format != tt.format {
				t.Errorf("type/format = %s/%s, want %s/%s", f.OpenAPIType, f.Format, tt.openAPIType, tt.format)
			}
			if f.Nullable != tt.nullable || f.OmitEmpty != tt.omitEmpty {
				t.Errorf("nullable/omitEmpty = %v/%v, want %v/%v", f.Nullable, f.OmitEmpty, tt.nullable, tt.omitEmpty)
			}
			if !slices.Equal(f.Index, tt.index) {
				t.Errorf("Index = %v, want %v", f.Index, tt.index)
			}
		})
	}

	for _, name := range []string{"Secret", "secret", "hidden", "Base", "Audit"} {
		if _, ok := m.FieldByJSON(name); ok {
			t.Errorf("FieldByJSON(%q) should not exist", name)
		}
	}
}

func TestIndexReachesValue(t *testing.T) {
	t.Parallel()
	m := mustOf[Todo](t)
	v := reflect.ValueOf(Todo{Base: Base{ID: 7}, Audit: &Audit{CreatedBy: "zaid"}, Title: "x"})
	for name, want := range map[string]any{"id": uint(7), "created_by": "zaid", "title": "x"} {
		f, _ := m.FieldByJSON(name)
		if got := v.FieldByIndex(f.Index).Interface(); got != want {
			t.Errorf("%s via Index = %v, want %v", name, got, want)
		}
	}
}

// TestMatchesEncodingJSON checks Fields against the keys encoding/json really
// writes, so the schema and the wire format can't drift apart.
func TestMatchesEncodingJSON(t *testing.T) {
	t.Parallel()
	note := "n"
	values := []any{
		Todo{Audit: &Audit{}, Note: &note},
		struct {
			outerA
			outerB
			Other string `json:"other"`
		}{},
		struct {
			outerTagged
			outerUntagged
		}{},
		struct {
			outerUntagged
			Name string
		}{},
	}
	for _, v := range values {
		m, err := brschema.For(reflect.TypeOf(v))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			t.Fatal(err)
		}
		var want []string
		for _, f := range m.Fields {
			if !f.OmitEmpty {
				want = append(want, f.JSONName)
			}
		}
		got := slices.Sorted(maps.Keys(obj))
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%T: encoding/json keys %v, schema fields %v", v, got, want)
		}
	}
}

func TestDBTags(t *testing.T) {
	t.Parallel()
	type tagged struct {
		Code   string `json:"code" db:"sku,pk"`
		Name   string `json:"name" db:"display_name" gorm:"column:ignored"`
		Legacy string `json:"legacy" gorm:"column:old_name"`
		ID     int    `json:"id"`
	}
	m, err := brschema.Of[tagged]()
	if err != nil {
		t.Fatal(err)
	}
	if m.PrimaryKey == nil || m.PrimaryKey.JSONName != "code" || m.PrimaryKey.Column != "sku" {
		t.Errorf("primary key = %+v, want code/sku from db tag (over the field named ID)", m.PrimaryKey)
	}
	for name, want := range map[string]string{"name": "display_name", "legacy": "old_name", "id": "id"} {
		if f, _ := m.FieldByJSON(name); f.Column != want {
			t.Errorf("%s column = %q, want %q", name, f.Column, want)
		}
	}

	type pkOnly struct {
		Key string `json:"key" db:",pk"`
	}
	if m, _ := brschema.Of[pkOnly](); m.PrimaryKey == nil || m.PrimaryKey.Column != "key" {
		t.Errorf(`db:",pk" without a name: %+v`, m.PrimaryKey)
	}
}

func TestPrimaryKey(t *testing.T) {
	t.Parallel()

	type Tagged struct {
		ID   int    `json:"id"`
		Code string `json:"code" gorm:"primary_key"`
	}
	type ByName struct {
		ID   int    `json:"identifier"`
		Name string `json:"name"`
	}
	type None struct {
		Name string `json:"name"`
	}

	tests := []struct {
		name string
		got  *brschema.Model
		want string
	}{
		{"tag wins over ID", mustOf[Tagged](t), "code"},
		{"field named ID", mustOf[ByName](t), "identifier"},
		{"promoted from embedded struct", mustOf[Todo](t), "id"},
		{"no primary key", mustOf[None](t), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ""
			if tt.got.PrimaryKey != nil {
				got = tt.got.PrimaryKey.JSONName
				if !tt.got.PrimaryKey.PrimaryKey {
					t.Error("PrimaryKey field does not have PrimaryKey set")
				}
			}
			if got != tt.want {
				t.Errorf("primary key = %q, want %q", got, tt.want)
			}
		})
	}
}

type outerA struct {
	Name string
}

type outerB struct {
	Name string
}

type outerTagged struct {
	Name string `json:"Name"`
}

type outerUntagged struct {
	Name string
}

func TestEmbeddedConflicts(t *testing.T) {
	t.Parallel()

	type Shallow struct {
		outerUntagged
		Name string
	}
	type Ambiguous struct {
		outerA
		outerB
		Other string `json:"other"`
	}
	type TaggedWins struct {
		outerTagged
		outerUntagged
	}
	type NamedEmbed struct {
		Base `json:"base"`
	}
	type Self struct {
		*Self
		Name string `json:"name"`
	}

	tests := []struct {
		name  string
		model *brschema.Model
		want  []string
		index map[string][]int
	}{
		{"shallower field wins", mustOf[Shallow](t), []string{"Name"}, map[string][]int{"Name": {1}}},
		{"same depth, none tagged: dropped", mustOf[Ambiguous](t), []string{"other"}, nil},
		{"same depth one tagged: tagged wins", mustOf[TaggedWins](t), []string{"Name"}, map[string][]int{"Name": {0, 0}}},
		{"embedded with json name is a nested object", mustOf[NamedEmbed](t), []string{"base"}, nil},
		{"recursive embedding terminates", mustOf[Self](t), []string{"name"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := jsonNames(tt.model.Fields); !slices.Equal(got, tt.want) {
				t.Errorf("fields = %v, want %v", got, tt.want)
			}
			for name, idx := range tt.index {
				f, _ := tt.model.FieldByJSON(name)
				if !slices.Equal(f.Index, idx) {
					t.Errorf("%s Index = %v, want %v", name, f.Index, idx)
				}
			}
		})
	}
	if f, _ := mustOf[NamedEmbed](t).FieldByJSON("base"); f.OpenAPIType != "object" {
		t.Errorf("named embed type = %s, want object", f.OpenAPIType)
	}
}

func TestErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		typ     reflect.Type
		wantErr string
	}{
		{"not a struct", reflect.TypeFor[string](), "string is not a struct"},
		{"nil type", nil, "nil type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := brschema.For(tt.typ)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestCache(t *testing.T) {
	t.Parallel()
	first := mustOf[Todo](t)
	if mustOf[*Todo](t) != first {
		t.Error("pointer type returned a different Model")
	}

	var wg sync.WaitGroup
	results := make([]*brschema.Model, 16)
	for i := range results {
		wg.Go(func() { results[i] = mustOf[Access](t) })
	}
	wg.Wait()
	for _, m := range results {
		if m != results[0] {
			t.Fatal("concurrent callers got different Models")
		}
	}

	type badTag struct {
		ID int64 `json:"id" bridgr:"nope"`
	}
	_, err1 := brschema.Of[badTag]()
	_, err2 := brschema.Of[badTag]()
	if err1 == nil || err1 != err2 { //nolint:errorlint // checks the cached error is reused
		t.Errorf("cached error not reused: %v vs %v", err1, err2)
	}
}

func TestSnakeCase(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"ID":         "id",
		"UserID":     "user_id",
		"CreatedAt":  "created_at",
		"HTTPServer": "http_server",
		"APIKeyV2":   "api_key_v2",
		"Name":       "name",
		"already":    "already",
		"Field1Name": "field1_name",
	}
	for in, want := range tests {
		if got := brschema.SnakeCase(in); got != want {
			t.Errorf("SnakeCase(%q) = %q, want %q", in, got, want)
		}
	}
}

func BenchmarkOfCached(b *testing.B) {
	if _, err := brschema.Of[Todo](); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = brschema.Of[Todo]()
	}
}

func BenchmarkFieldLookup(b *testing.B) {
	m, err := brschema.Of[Todo]()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = m.FieldByJSON("user_id")
	}
}

type Access struct {
	ID        int64     `json:"id" bridgr:"readonly"`
	Password  string    `json:"password,omitzero" bridgr:"writeonly"`
	CreatedAt time.Time `json:"created_at" bridgr:" readonly ,"`
	Name      string    `json:"name" bridgr:""`
}

func TestAccessTags(t *testing.T) {
	t.Parallel()
	m := mustOf[Access](t)
	if got := jsonNames(m.ReadOnly); !slices.Equal(got, []string{"id", "created_at"}) {
		t.Errorf("ReadOnly = %v", got)
	}
	if got := jsonNames(m.WriteOnly); !slices.Equal(got, []string{"password"}) {
		t.Errorf("WriteOnly = %v", got)
	}
	if f, _ := m.FieldByJSON("name"); f.ReadOnly || f.WriteOnly {
		t.Errorf("name = %+v, want no access flags", f)
	}
}

type pointerBase struct {
	Secret string `json:"secret,omitzero" bridgr:"writeonly"`
}

func TestAccessTagErrors(t *testing.T) {
	t.Parallel()
	type unknown struct {
		ID   int64  `json:"id"`
		Name string `json:"name" bridgr:"hidden"`
	}
	type both struct {
		ID   int64  `json:"id"`
		Name string `json:"name,omitzero" bridgr:"readonly,writeonly"`
	}
	type viaPointer struct {
		ID int64 `json:"id"`
		*pointerBase
	}
	type writeOnlyKey struct {
		ID int64 `json:"id,omitzero" bridgr:"writeonly"`
	}
	type alwaysEncoded struct {
		ID       int64  `json:"id"`
		Password string `json:"password" bridgr:"writeonly"`
	}
	tests := []struct {
		name string
		typ  reflect.Type
		want string
	}{
		{"unknown option", reflect.TypeFor[unknown](), `unknown bridgr tag option "hidden"`},
		{"readonly and writeonly", reflect.TypeFor[both](), "both readonly and writeonly"},
		{"embedded pointer", reflect.TypeFor[viaPointer](), "embedded pointer struct"},
		{"writeonly key", reflect.TypeFor[writeOnlyKey](), "primary key, so it can't be writeonly"},
		{"writeonly without omitzero", reflect.TypeFor[alwaysEncoded](), "needs omitzero"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := brschema.For(tt.typ); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("For = %v, want an error containing %q", err, tt.want)
			}
		})
	}
}

func TestNestedWriteOnly(t *testing.T) {
	t.Parallel()
	type node struct {
		Next *node `json:"next"`
	}
	type deep struct {
		Inner struct {
			Users map[string][]*Access `json:"users"`
		} `json:"inner"`
	}
	tests := []struct {
		typ  reflect.Type
		want string
	}{
		{reflect.TypeFor[Access](), ""},
		{reflect.TypeFor[[]*Access](), ""},
		{reflect.TypeFor[node](), ""},
		{reflect.TypeFor[Todo](), ""},
		{reflect.TypeFor[struct {
			Owner Access `json:"owner"`
		}](), ".Owner"},
		{reflect.TypeFor[[]deep](), "deep.Inner.Users"},
	}
	for _, tt := range tests {
		got, ok := brschema.NestedWriteOnly(tt.typ)
		if got != tt.want || ok != (tt.want != "") {
			t.Errorf("NestedWriteOnly(%s) = %q, %v; want %q", tt.typ, got, ok, tt.want)
		}
	}
}
