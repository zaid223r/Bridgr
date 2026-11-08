// Package models defines the resources served by the todo example.
package models

// Note is a free-text note. Only its title can be filtered.
type Note struct {
	ID    uint   `json:"id" gorm:"primaryKey"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// FilterableFields limits list filtering to the title.
func (Note) FilterableFields() []string {
	return []string{"title"}
}
