package api

import (
	"sync"
)

var (
	openAPIRoutes = make(map[string]*PathInfo)
	mutex         = sync.Mutex{}
)

// PathInfo describes one documented operation: its HTTP method, path pattern and model.
type PathInfo struct {
	Method string
	Path   string
	Model  any
}

// RegisterPath records an operation so it appears in the generated OpenAPI document.
// Registering the same method and path twice keeps the last model. It panics if
// model is not a valid model, so BuildSpec never fails on a registered one.
func RegisterPath(method, path string, model any) {
	mustSchema(model)
	mutex.Lock()
	defer mutex.Unlock()
	openAPIRoutes[method+" "+path] = &PathInfo{
		Method: method,
		Path:   path,
		Model:  model,
	}
}

// GetPaths returns a snapshot of all registered operations in no particular order.
func GetPaths() []*PathInfo {
	mutex.Lock()
	defer mutex.Unlock()

	var list []*PathInfo
	for _, v := range openAPIRoutes {
		list = append(list, v)
	}
	return list
}
