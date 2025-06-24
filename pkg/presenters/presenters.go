package presenters

import (
	"go.uber.org/zap"
)

type presenters struct {
	log *zap.Logger
}

func NewPresenters(log *zap.Logger) Presenters {
	return &presenters{log: log.Named("presenters")}
}
