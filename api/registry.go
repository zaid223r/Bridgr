// Package api builds the OpenAPI 3.1 document for the resources registered on a
// router and serves the docs UI.
package api

import (
	"encoding/json"
	"sync"

	"github.com/zaid223r/Bridgr/core/brschema"
)

// Info is the document's info object.
type Info struct {
	Title       string
	Version     string
	Description string
}

// CRUDDocs says which optional behaviour a CRUD resource has, so the document
// only lists responses that can actually happen.
type CRUDDocs struct {
	// Auth adds 401 and 403 responses to every operation.
	Auth bool
	// Validate adds 422 responses to create, replace and update.
	Validate bool
	// DefaultLimit and MaxLimit document the limit parameter.
	DefaultLimit int
	MaxLimit     int
}

type resource struct {
	path  string
	model *brschema.Model
	docs  CRUDDocs
}

// Registry collects the resources to document. Each router owns one; it is
// safe for concurrent use.
type Registry struct {
	mu        sync.Mutex
	resources []resource
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// AddCRUD documents the six CRUD operations of a resource mounted at /path.
func (r *Registry) AddCRUD(path string, model *brschema.Model, docs CRUDDocs) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resources = append(r.resources, resource{path: path, model: model, docs: docs})
}

// Build renders the OpenAPI 3.1 document as indented JSON. The output is
// deterministic: the same registrations always produce the same bytes.
func (r *Registry) Build(info Info) ([]byte, error) {
	r.mu.Lock()
	resources := append([]resource(nil), r.resources...)
	r.mu.Unlock()

	g := newSchemaGen()
	paths := map[string]any{}
	tags := make([]map[string]any, 0, len(resources))
	for _, res := range resources {
		for path, item := range g.pathItems(res) {
			paths[path] = item
		}
		tags = append(tags, map[string]any{"name": res.path})
	}

	if info.Title == "" {
		info.Title = "Bridgr API"
	}
	if info.Version == "" {
		info.Version = "1.0.0"
	}
	infoObj := map[string]any{"title": info.Title, "version": info.Version}
	if info.Description != "" {
		infoObj["description"] = info.Description
	}

	doc := map[string]any{
		"openapi": "3.1.0",
		"info":    infoObj,
		"tags":    tags,
		"paths":   paths,
		"components": map[string]any{
			"schemas":   g.components,
			"responses": problemResponses(),
		},
	}
	return json.MarshalIndent(doc, "", "  ")
}
