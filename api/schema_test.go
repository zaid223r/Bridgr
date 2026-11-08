package api

import (
	"encoding/json"
	"testing"
)

type specTodo struct {
	ID     uint    `json:"id" db:",pk"`
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
	RegisterPath("patch", "/spec-todos/{id}", specTodo{})

	raw, err := BuildSpec()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name        string         `json:"name"`
				In          string         `json:"in"`
				Description string         `json:"description"`
				Schema      map[string]any `json:"schema"`
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
	descriptions := map[string]string{}
	for _, p := range list.Parameters {
		descriptions[p.Name] = p.Description
	}
	if want := "Case-insensitive substring match. Repeat to match any of several values. Operators: title[eq], title[ne], title[contains], title[in]. title[in] takes a comma-separated list."; descriptions["title"] != want {
		t.Errorf("title description = %q", descriptions["title"])
	}
	if want := "Exact match. Repeat to match any of several values. Operators: id[eq], id[ne], id[gt], id[gte], id[lt], id[lte], id[in]. id[in] takes a comma-separated list."; descriptions["id"] != want {
		t.Errorf("id description = %q", descriptions["id"])
	}

	post := doc.Paths["/spec-todos"]["post"].RequestBody.Content["application/json"].Schema.Properties
	if _, ok := post["id"]; ok {
		t.Error("POST body should not contain the primary key")
	}
	for _, method := range []string{"put", "patch"} {
		body := doc.Paths["/spec-todos/{id}"][method].RequestBody.Content["application/json"].Schema.Properties
		if body["id"]["readOnly"] != true || body["id"]["type"] != "integer" || body["title"] == nil {
			t.Errorf("%s body = %v, want all fields with a readOnly integer id", method, body)
		}
	}
}

type typoSpec struct {
	Title string `json:"title"`
}

func (typoSpec) FilterableFields() []string { return []string{"titel"} }

func TestRegisterPathRejectsInvalidModel(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("RegisterPath accepted a model with an unknown filterable field")
		}
	}()
	RegisterPath("get", "/typo-spec", typoSpec{})
}
