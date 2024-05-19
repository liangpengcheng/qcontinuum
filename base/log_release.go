package base

func LogDebug(format string, v ...interface{}) {
	//_logFormat("DEBUG ", format, v...)
}

func LogInfo(format string, v ...interface{}) {
	// _logFormat(" INFO ", format, v...)
	Zap().Sugar().Infof(format, v)
}

func LogWarn(format string, v ...interface{}) {
	// _logFormat(Yellow(" WARN "), format, v...)
	Zap().Sugar().Warnf(format, v)
}

func LogError(format string, v ...interface{}) {
	// _logFormat(Red("ERROR "), format, v...)
	Zap().Sugar().Errorf(format, v)
}

func LogPanic(format string, v ...interface{}) {
	Zap().Sugar().Panicf(format, v)
}
