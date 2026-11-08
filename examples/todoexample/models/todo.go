// Package models defines the resources served by the todo example.
package models

// Todo is a task that can be marked done. Every field can be filtered.
type Todo struct {
	ID    int64  `json:"id" db:",pk"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

// GenericFiltering opts Todo into filtering on all fields.
func (Todo) GenericFiltering() bool { return true }
