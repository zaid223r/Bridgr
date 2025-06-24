package brorm

import (
	"gorm.io/gorm"
)

type GormModel[T any] struct {
	DB *gorm.DB
}

func (m *GormModel[T]) List() ([]T, error) {
	var result []T
	err := m.DB.Find(&result).Error
	return result, err
}

func (m *GormModel[T]) Get(id string) (T, error) {
	var result T
	err := m.DB.First(&result, "id = ?", id).Error
	return result, err
}

func (m *GormModel[T]) Create(input T) (T, error) {
	err := m.DB.Create(&input).Error
	return input, err
}

func (m *GormModel[T]) Update(id string, input T) (T, error) {
	// update by replacing
	var existing T
	if err := m.DB.First(&existing, "id = ?", id).Error; err != nil {
		return input, err
	}
	err := m.DB.Model(&existing).Updates(input).Error
	return existing, err
}

func (m *GormModel[T]) Delete(id string) error {
	var obj T
	if err := m.DB.Where("id = ?", id).Delete(&obj).Error; err != nil {
		return err
	}
	return nil
}
