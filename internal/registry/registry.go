package registry

type Register interface {
	Register() (*Registry, error)
}

type Registry struct {
	Method  string
	Service string
	Addr    string
}
