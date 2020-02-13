package base

import (
	"fmt"
	"log"
	"os"
)

var logFile *os.File
var logger *log.Logger
var logHead string

// InitLogFile 初始化Log 文件，不调用的话，就不会写入文件
func InitLogFile(filename string) {
	var err error
	logFile, err = os.OpenFile(filename+".log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Panicf("open log file:%s.log error:%s", filename, err)
	}
	log.SetFlags(log.Ldate | log.Lmicroseconds)
	logger = log.New(logFile, "", log.Ldate|log.Lmicroseconds)
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
