package crud

import (
	"bridgr/core/context"
	brhttp "bridgr/core/http"
	"encoding/json"
	"net/http"
)
func RegisterCRUDRoutes(router *brhttp.Router, path string, model BridgrModel){
	base := "/" + path

	router.AddRoute("GET", base, func(w http.ResponseWriter, r *http.Request){
		context.JSON(w, 200, model.List())
	})

	router.AddRoute("POST", base, func(w http.ResponseWriter, r *http.Request){
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil{
			context.JSON(w , 400, map[string]string{"error":"Invalid input"})
			return
		}
		created, err:= model.Create(input)
		if err != nil {
			context.JSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		context.JSON(w, 201, created)
	})


	router.AddRoute("GET", base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len(base)+1:]
		item, err := model.Get(id)
		if err != nil {
			context.JSON(w, 404, map[string]string{"error": "Not found"})
			return
		}
		context.JSON(w, 200, item)
	})

	router.AddRoute("PUT", base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len(base)+1:]
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			context.JSON(w, 400, map[string]string{"error": "Invalid input"})
			return
		}
		updated, err := model.Update(id, input)
		if err != nil {
			context.JSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		context.JSON(w, 200, updated)
	})

	router.AddRoute("DELETE", base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len(base)+1:]
		err := model.Delete(id)
		if err != nil {
			context.JSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		context.JSON(w, 204, nil)
	})
}