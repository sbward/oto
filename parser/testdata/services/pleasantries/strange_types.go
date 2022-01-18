package pleasantries

type StrangeTypesService interface {
	DoSomethingStrange(DoSomethingStrangeRequest) DoSomethingStrangeResponse
}

type DoSomethingStrangeRequest struct {
	Anything interface{}
	Alias    Alias
	Private  int `json:"-"`
}

type DoSomethingStrangeResponse struct {
	Value interface{}
	Size  int
}

type Alias string
