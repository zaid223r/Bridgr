package brcrud

type BridgrModel[T any] interface {
	List(filters map[string][]string) ([]T, error)
	Get(id string) (T, error)
	Create(input T) (T, error)
	Update(id string, input T) (T, error)
	Delete(id string) error
}
