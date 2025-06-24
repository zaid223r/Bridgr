package brorm

import (
	"reflect"

	"github.com/zaid223r/Bridgr/errors"

	"gorm.io/gorm"
)

// Global flag to enable generic filtering for all fields
var GenericFilteringEnabled = false

type GormModel[T any] struct {
	DB *gorm.DB
}

func (m *GormModel[T]) List(filters map[string][]string) ([]T, error) {
	var result []T
	allowed := getFilterableFields[T]()
	var zero T
	typeOf := reflect.TypeOf(zero)
	if typeOf.Kind() == reflect.Ptr {
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
		return nil, &errors.InvalidFilterFieldError{Fields: invalidFields}
	}
	err := db.Find(&result).Error
	return result, err
}

// getFilterableFields returns a map of allowed filter fields for the model T
func getFilterableFields[T any]() map[string]bool {
	var zero T
	allowed := map[string]bool{}
	typeOf := reflect.TypeOf(zero)
	if typeOf.Kind() == reflect.Ptr {
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
