package context

import (
	"encoding/json"
	"net/http"
)

func JSON(w http.ResponseWriter, status int, data any){
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}