package extra

import (
	"context"
)

type AcclaimerService interface {
	Acclaim(context.Context, *AcclaimRequest) (*AcclaimResponse, error)
}

type AcclaimRequest struct {
	Name       string
	Reason     string
	Enthusiasm int
}

type AcclaimResponse struct {
	Acclamation string `json:"acclamation,omitempty"`
}
