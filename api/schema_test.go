package api

import (
	"encoding/json"
	"testing"
)

type specTodo struct {
	ID     uint    `json:"id" gorm:"primaryKey"`
	Title  string  `json:"title"`
	Note   *string `json:"note"`
	Secret string  `json:"-"`
}

func (specTodo) FilterableFields() []string { return []string{"title", "id"} }

func TestReflectModel(t *testing.T) {
	t.Parallel()
	props := ReflectModel(specTodo{})

	want := map[string]map[string]any{
		"id":    {"type": "integer", "format": "int64"},
		"title": {"type": "string"},
		"note":  {"type": "string", "nullable": true},
	}
	if len(props) != len(want) {
		t.Fatalf("props = %v, want keys of %v", props, want)
	}
	for name, w := range want {
		got, _ := props[name].(map[string]any)
		for k, v := range w {
			if got[k] != v {
				t.Errorf("%s.%s = %v, want %v", name, k, got[k], v)
			}
		}
	}
}

func TestReflectModelPanicsOnInvalidModel(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("expected a panic for a non-struct model")
		}
	}()
	ReflectModel("not a struct")
}

func TestBuildSpecUsesSchema(t *testing.T) {
	RegisterPath("get", "/spec-todos", specTodo{})
	RegisterPath("post", "/spec-todos", specTodo{})
	RegisterPath("put", "/spec-todos/{id}", specTodo{})

	raw, err := BuildSpec()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name   string         `json:"name"`
				In     string         `json:"in"`
				Schema map[string]any `json:"schema"`
			} `json:"parameters"`
			RequestBody struct {
				Content map[string]struct {
					Schema struct {
						Properties map[string]map[string]any `json:"properties"`
					} `json:"schema"`
				} `json:"content"`
			} `json:"requestBody"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}

	list := doc.Paths["/spec-todos"]["get"]
	var names []string
	for _, p := range list.Parameters {
		names = append(names, p.Name)
		if p.Name == "id" && p.Schema["type"] != "integer" {
			t.Errorf("id filter type = %v, want integer", p.Schema["type"])
		}
	}
	if len(names) != 2 || names[0] != "title" || names[1] != "id" {
		t.Errorf("list filters = %v, want [title id]", names)
	}

	post := doc.Paths["/spec-todos"]["post"].RequestBody.Content["application/json"].Schema.Properties
	if _, ok := post["id"]; ok {
		t.Error("POST body should not contain the primary key")
	}
	put := doc.Paths["/spec-todos/{id}"]["put"].RequestBody.Content["application/json"].Schema.Properties
	if put["id"]["readOnly"] != true || put["id"]["type"] != "integer" {
		t.Errorf("PUT id = %v, want readOnly integer", put["id"])
	}
}
