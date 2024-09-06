package base

import (
	"flag"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var zp *zap.Logger

func Zap() *zap.Logger {
	return zp
}
func init() {

	logPath := flag.String("logpath", "", "log file path")
	flag.Parse()
	if logPath != nil && *logPath != "" {
		// 创建文件写入器
		file, _ := os.Create(*logPath)
		writeSyncer := zapcore.AddSync(file)

		// 配置日志编码器
		encoderConfig := zap.NewProductionEncoderConfig()
		encoderConfig.TimeKey = "time"
		encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

		// 设置日志级别和输出目标
		core := zapcore.NewCore(
			zapcore.NewJSONEncoder(encoderConfig), // 使用 JSON 编码器
			writeSyncer,                           // 输出到指定文件
			zap.InfoLevel,                         // 日志级别
		)

		zp = zap.New(core)
		defer zp.Sync()
	} else {
		zp, _ = zap.NewProduction()
	}

	//

}
