package crud

type BridgrModel interface{
	List() any
	Get(id string) (any, error)
	Create(input any) (any, error)
	Update(id string, input any) (any, error)
	Delete(id string) error
}