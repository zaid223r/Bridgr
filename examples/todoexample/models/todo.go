package models

type Todo struct {
	ID    uint   `json:"id" gorm:"primaryKey"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

func (Todo) GenericFiltering() bool {
	return true
}