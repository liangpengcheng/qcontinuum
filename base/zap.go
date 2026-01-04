package base

import (
	"flag"
	"time"

	"github.com/natefinch/lumberjack"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var zp *zap.Logger

// 日志目录路径通过命令行参数传递，定义在包级别
var logDir = flag.String("logpath", "", "log directory path")

func Zap() *zap.Logger {
	return zp
}

// InitZap 初始化 zap 日志，必须在 flag.Parse() 之后调用
func InitZap() {
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
	// 默认初始化一个 production logger
	// 如果需要使用 -logpath 参数，请在 flag.Parse() 之后调用 InitZap()
	zp, _ = zap.NewProduction()
}
