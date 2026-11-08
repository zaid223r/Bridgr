package models

// Todo is a task that can be marked done. Every field can be filtered.
type Todo struct {
	ID    uint   `json:"id" gorm:"primaryKey"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

// GenericFiltering opts Todo into filtering on all fields.
func (Todo) GenericFiltering() bool {
	return true
}
