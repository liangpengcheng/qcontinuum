package base

import (
	"flag"
	"time"

	"github.com/natefinch/lumberjack"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var zp *zap.Logger

func Zap() *zap.Logger {
	return zp
}
func init_zap() {
	// 日志目录路径通过命令行参数传递
	logDir := flag.String("logpath", "", "log directory path")
	flag.Parse()

	if logDir != nil && *logDir != "" {
		// 使用 lumberjack 进行日志轮转
		writeSyncer := zapcore.AddSync(&lumberjack.Logger{
			Filename:   *logDir + "/log-" + time.Now().Format("2006-01-02") + ".log", // 按日期命名日志文件
			MaxSize:    10,                                                           // 每个日志文件最大 10 MB
			MaxBackups: 30,                                                           // 保留 30 个备份
			MaxAge:     7,                                                            // 保留 7 天的日志文件
			Compress:   false,                                                        // 不压缩旧日志
		})

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
}
func init() {
	init_zap()

	//

}
