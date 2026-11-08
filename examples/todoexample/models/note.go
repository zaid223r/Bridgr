package models

// Note is a free-text note. Only its title can be filtered.
type Note struct {
	ID    int64  `json:"id" db:",pk"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// FilterableFields limits list filtering to the title.
func (Note) FilterableFields() []string {
	return []string{"title"}
}
