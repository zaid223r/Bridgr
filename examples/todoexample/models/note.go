package models

type Note struct {
	ID    uint   `json:"id" gorm:"primaryKey"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

func (Note) FilterableFields() []string {
	return []string{"title"}
}
