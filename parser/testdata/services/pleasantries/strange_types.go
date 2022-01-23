package pleasantries

import "time"

type StrangeTypesService interface {
	DoSomethingStrange(DoSomethingStrangeRequest) DoSomethingStrangeResponse
}

type DoSomethingStrangeRequest struct {
	Anything     interface{}
	Alias        Alias
	Private      int `json:"-"`
	Time         time.Time
	OptionalTime *time.Time `json:",omitempty"`
	InfiniteLoop *InfiniteLoop
}

type InfiniteLoop struct {
	*InfiniteLoop
}

type DoSomethingStrangeResponse struct {
	Value interface{}
	Size  int
}

type Alias string
