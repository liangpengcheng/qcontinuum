package base

import (
	"fmt"
	"log"
	"os"
	"time"
)

var logFile *os.File
var logger *log.Logger
var logHead string

// InitLogFile 初始化Log 文件，不调用的话，就不会写入文件
func InitLogFile(filename string) {
	var err error
	// 使用当前日期作为日志文件名后缀
	currentDate := time.Now().Format("2006-01-02")
	logFileName := fmt.Sprintf("%s_%s.log", filename, currentDate)

	logFile, err = os.OpenFile(logFileName, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Panicf("open log file:%s error:%s", logFileName, err)
	}
	log.SetFlags(log.Ldate | log.Lmicroseconds)
	logger = log.New(logFile, "", log.Ldate|log.Lmicroseconds)

	// 每天凌晨创建新的日志文件
	go func() {
		for {
			now := time.Now()
			// 计算下一个凌晨的时间
			next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
			timer := time.NewTimer(next.Sub(now))
			<-timer.C

			// 关闭当前日志文件
			if logFile != nil {
				logFile.Close()
			}

			// 创建新的日志文件
			currentDate = time.Now().Format("2006-01-02")
			logFileName = fmt.Sprintf("%s_%s.log", filename, currentDate)
			logFile, err = os.OpenFile(logFileName, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
			if err != nil {
				log.Printf("Failed to create new log file: %v", err)
				continue
			}
			logger = log.New(logFile, "", log.Ldate|log.Lmicroseconds)
		}
	}()
}

// CloseLogFile 关闭日志文件
func CloseLogFile() {
	if logFile != nil {
		logFile.Close()
	}
}

func _logFormat(prefix string, format string, v ...interface{}) {
	s := fmt.Sprintf(logHead+prefix+format, v...)
	if logger != nil {
		logger.Printf(s)
	}
	consoleLog(s)
}
func InitLog(system string) {
	logHead = textColor(TextMagenta, "["+system+"]:")
}
