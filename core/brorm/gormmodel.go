// Package brorm implements brcrud.BridgrModel on top of GORM.
package brorm

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"

	"github.com/zaid223r/Bridgr/core/brerrors"
	"github.com/zaid223r/Bridgr/core/brschema"

	"gorm.io/gorm"
)

// GenericFilteringEnabled makes every JSON field of every model filterable.
// Scheduled for removal; use GenericFiltering on the model instead.
var GenericFilteringEnabled = false

// GormModel stores values of T in the table GORM derives from T.
type GormModel[T any] struct {
	DB *gorm.DB
}

// List returns all rows matching filters. String fields match with LIKE, other
// fields by equality. Keys that are not filterable produce an InvalidFilterFieldError
// listing them in sorted order.
func (m *GormModel[T]) List(filters map[string][]string) ([]T, error) {
	model, err := brschema.Of[T]()
	if err != nil {
		return nil, err
	}
	db := m.DB
	var invalidFields []string
	for _, key := range slices.Sorted(maps.Keys(filters)) {
		field, ok := model.FilterField(key)
		if !ok && GenericFilteringEnabled {
			field, ok = model.FieldByJSON(key)
		}
		if !ok {
			invalidFields = append(invalidFields, key)
			continue
		}
		value := filters[key][0]
		if field.Type.Kind() == reflect.String {
			db = db.Where(field.Column+" LIKE ?", "%"+value+"%")
		} else {
			db = db.Where(field.Column+" = ?", value)
		}
	}
	if len(invalidFields) > 0 {
		return nil, &brerrors.InvalidFilterFieldError{Fields: invalidFields}
	}
	var result []T
	err = db.Find(&result).Error
	return result, err
}

// Get returns the row whose primary key is id, or an error wrapping
// brerrors.ErrNotFound.
func (m *GormModel[T]) Get(id string) (T, error) {
	var result T
	err := m.DB.First(&result, "id = ?", id).Error
	return result, mapError(err)
}

// Create inserts input and returns it with generated fields such as the ID filled in.
func (m *GormModel[T]) Create(input T) (T, error) {
	err := m.DB.Create(&input).Error
	return input, err
}

// Update applies input to the row with primary key id, or returns an error wrapping
// brerrors.ErrNotFound. Zero-valued fields are skipped (GORM Updates semantics), so
// they cannot be cleared this way.
func (m *GormModel[T]) Update(id string, input T) (T, error) {
	var existing T
	if err := m.DB.First(&existing, "id = ?", id).Error; err != nil {
		return input, mapError(err)
	}
	err := m.DB.Model(&existing).Updates(input).Error
	return existing, err
}

// Delete removes the row with primary key id, or returns an error wrapping
// brerrors.ErrNotFound when no row matched.
func (m *GormModel[T]) Delete(id string) error {
	var obj T
	res := m.DB.Where("id = ?", id).Delete(&obj)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("deleting id %s: %w", id, brerrors.ErrNotFound)
	}
	return nil
}

// mapError translates GORM's not-found error into brerrors.ErrNotFound and keeps
// the original in the chain.
func mapError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w: %w", brerrors.ErrNotFound, err)
	}
	return err
}
