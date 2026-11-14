// Package models defines the resources served by the todo example, one file
// per table: the model and how its endpoints behave.
package models

import bridgr "github.com/zaid223r/Bridgr"

// Todo is a task that can be marked done.
type Todo struct {
	ID    int64  `json:"id" db:",pk"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

// TodoOptions lets clients filter by title and done, and sort by title.
var TodoOptions = &bridgr.CRUDOptions[Todo]{
	Filters: bridgr.Filters{
		"title": bridgr.Exact | bridgr.Contains, // ?title=x, ?title[contains]=x
		"done":  bridgr.Exact,                   // ?done=false
	},
	Sort: []string{"title"},
}
