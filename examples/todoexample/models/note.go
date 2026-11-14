package models

import bridgr "github.com/zaid223r/Bridgr"

// Note is a free-text note.
type Note struct {
	ID    int64  `json:"id" db:",pk"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// NoteOptions lets clients search notes by title only.
var NoteOptions = &bridgr.CRUDOptions[Note]{
	Filters: bridgr.Filters{"title": bridgr.Contains}, // ?title=milk matches substrings
	Sort:    []string{"title"},
}
