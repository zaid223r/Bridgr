package crud

type BridgrModel[T any] interface {
	List() ([]T, error)
	Get(id string) (T, error)
	Create(input T) (T, error)
	Update(id string, input T) (T, error)
	Delete(id string) error
}
