package api

import (
	"sync"
)

var (
	openAPIRoutes = make(map[string]*PathInfo)
	mutex         = sync.Mutex{}
)

type PathInfo struct {
	Method string
	Path   string
	Model  any
}

func RegisterPath(method, path string, model any) {
	mutex.Lock()
	defer mutex.Unlock()
	openAPIRoutes[method+" "+path] = &PathInfo{
		Method: method,
		Path:   path,
		Model:  model,
	}
}

func GetPaths() []*PathInfo {
	mutex.Lock()
	defer mutex.Unlock()

	var list []*PathInfo
	for _, v := range openAPIRoutes {
		list = append(list, v)
	}
	return list
}
