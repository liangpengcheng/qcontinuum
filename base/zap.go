package base

import (
	"go.uber.org/zap"
)

var zp *zap.Logger

func Zap() *zap.Logger {
	return zp
}
func init() {
	zp, _ = zap.NewProduction()
	defer zp.Sync()
}
