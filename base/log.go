package base

import (
	"fmt"
	"log"
	"os"
)

var logFile *os.File
var logger *log.Logger

func InitLogFile(filename string) {
	var err error
	logFile, err = os.OpenFile(filename+".log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Panicf("open log file:%s.log error:%s", filename, err)
	}
	logger = log.New(logFile, "", log.LstdFlags)
}

func CloseLogFile() {
	if logFile != nil {
		logFile.Close()
	}
}

func _logFormat(prefix string, format string, v ...interface{}) {
	s := fmt.Sprintf(prefix+format, v...)
	logger.Printf(s)
	consoleLog(s)
}
