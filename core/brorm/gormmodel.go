// Package brorm implements brcrud.BridgrModel on top of GORM.
package brorm

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/zaid223r/Bridgr/core/brerrors"

	"gorm.io/gorm"
)

// GenericFilteringEnabled makes every query parameter a filter for every model.
// Unsafe: query keys become column names in SQL. Scheduled for removal.
var GenericFilteringEnabled = false

// GormModel stores values of T in the table GORM derives from T.
type GormModel[T any] struct {
	DB *gorm.DB
}

// List returns all rows matching filters. String fields match with LIKE, other
// fields by equality. Keys that are not filterable produce an InvalidFilterFieldError.
func (m *GormModel[T]) List(filters map[string][]string) ([]T, error) {
	var result []T
	allowed := getFilterableFields[T]()
	var zero T
	typeOf := reflect.TypeOf(zero)
	if typeOf.Kind() == reflect.Pointer {
		typeOf = typeOf.Elem()
	}
	db := m.DB
	var invalidFields []string
	for key, values := range filters {
		if GenericFilteringEnabled || allowed[key] {
			// Find the struct field by json tag or name
			var fieldType reflect.Type
			for i := 0; i < typeOf.NumField(); i++ {
				f := typeOf.Field(i)
				jsonTag := f.Tag.Get("json")
				if jsonTag == key || (jsonTag == "" && f.Name == key) {
					fieldType = f.Type
					break
				}
			}
			if fieldType.Kind() == reflect.String {
				db = db.Where(key+" LIKE ?", "%"+values[0]+"%")
			} else {
				db = db.Where(key+" = ?", values[0])
			}
		} else {
			invalidFields = append(invalidFields, key)
		}
	}
	if len(invalidFields) > 0 {
		return nil, &brerrors.InvalidFilterFieldError{Fields: invalidFields}
	}
	err := db.Find(&result).Error
	return result, err
}

// getFilterableFields returns a map of allowed filter fields for the model T
func getFilterableFields[T any]() map[string]bool {
	var zero T
	allowed := map[string]bool{}
	typeOf := reflect.TypeOf(zero)
	if typeOf.Kind() == reflect.Pointer {
		typeOf = typeOf.Elem()
	}

	// Check for GenericFiltering() bool
	if _, ok := reflect.TypeOf(zero).MethodByName("GenericFiltering"); ok {
		result := reflect.ValueOf(zero).MethodByName("GenericFiltering").Call(nil)
		if len(result) == 1 && result[0].Bool() {
			// Allow all fields
			for i := 0; i < typeOf.NumField(); i++ {
				field := typeOf.Field(i)
				jsonName := field.Tag.Get("json")
				if jsonName == "" {
					jsonName = field.Name
				}
				allowed[jsonName] = true
			}
			return allowed
		}
	}

	// Otherwise, check for FilterableFields()
	if _, ok := reflect.TypeOf(zero).MethodByName("FilterableFields"); ok {
		vals := reflect.ValueOf(zero).MethodByName("FilterableFields").Call(nil)
		if len(vals) > 0 {
			for _, v := range vals[0].Interface().([]string) {
				allowed[v] = true
			}
		}
	}
	return allowed
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
