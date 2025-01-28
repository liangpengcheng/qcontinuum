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
		// 配置日志编码器
		encoderConfig := zap.NewProductionEncoderConfig()
		encoderConfig.TimeKey = "time"
		encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

		// 创建初始日志文件
		currentDate := time.Now().Format("2006-01-02")
		filename := *logDir + "/log-" + currentDate + ".log"
		writeSyncer := zapcore.AddSync(&lumberjack.Logger{
			Filename:   filename,
			MaxSize:    500,   // 每个日志文件最大 500 MB
			MaxBackups: 30,    // 保留 30 个备份
			MaxAge:     7,     // 保留 7 天的日志文件
			Compress:   false, // 不压缩旧日志
		})

		// 设置日志级别和输出目标
		core := zapcore.NewCore(
			zapcore.NewJSONEncoder(encoderConfig),
			writeSyncer,
			zap.InfoLevel,
		)

		zp = zap.New(core)

		// 每天凌晨创建新的日志文件
		go func() {
			for {
				now := time.Now()
				next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
				timer := time.NewTimer(next.Sub(now))
				<-timer.C

				// 创建新的日志文件
				currentDate = time.Now().Format("2006-01-02")
				filename = *logDir + "/log-" + currentDate + ".log"

				writeSyncer := zapcore.AddSync(&lumberjack.Logger{
					Filename:   filename,
					MaxSize:    500,
					MaxBackups: 30,
					MaxAge:     7,
					Compress:   false,
				})

				core := zapcore.NewCore(
					zapcore.NewJSONEncoder(encoderConfig),
					writeSyncer,
					zap.InfoLevel,
				)

				zp = zap.New(core)
			}
		}()

	} else {
		zp, _ = zap.NewProduction()
	}
}
func init() {
	init_zap()

	//

}
