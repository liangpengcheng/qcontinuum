package base

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 模拟测试环境并验证日志文件生成和写入
func TestLogInitialization(t *testing.T) {
	// 创建一个临时目录，模拟日志目录
	tmpDir, err := ioutil.TempDir("", "log_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %s", err)
	}
	defer os.RemoveAll(tmpDir) // 测试结束后删除临时目录

	// 模拟将 logpath 指向临时目录
	os.Args = []string{"cmd", "-logpath", tmpDir}

	// 调用 init() 函数，初始化日志
	init_zap()
	// 检查日志文件是否按照预期生成
	currentDate := time.Now().Format("2006-01-02")
	logFileName := "log-" + currentDate + ".log"
	logFilePath := filepath.Join(tmpDir, logFileName)

	// 检查日志文件是否存在
	if _, err := os.Stat(logFilePath); os.IsNotExist(err) {
		t.Fatalf("Log file was not created: %s", logFilePath)
	}

	// 写入测试日志
	zp.Info("Test log entry")

	// 读取日志文件内容
	logContent, err := ioutil.ReadFile(logFilePath)
	if err != nil {
		t.Fatalf("Failed to read log file: %s", err)
	}

	// 验证日志文件内容是否包含预期的日志条目
	if !strings.Contains(string(logContent), "Test log entry") {
		t.Errorf("Expected log entry not found in log file. Got: %s", logContent)
	}
}
