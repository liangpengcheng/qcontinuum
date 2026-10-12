package base

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 模拟测试环境并验证日志文件生成和写入
func TestLogInitialization(t *testing.T) {
	// Run initialization in a child process to isolate flags, the global logger,
	// and the daily rotation goroutine from other tests.
	if os.Getenv("QCONTINUUM_LOG_TEST_CHILD") == "1" {
		InitZap()
		Zap().Info("Test log entry")
		if err := Zap().Sync(); err != nil {
			t.Fatal(err)
		}
		return
	}
	tmpDir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestLogInitialization$", "-logpath", tmpDir)
	cmd.Env = append(os.Environ(), "QCONTINUUM_LOG_TEST_CHILD=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Log initialization failed: %v\n%s", err, output)
	}

	currentDate := time.Now().Format("2006-01-02")
	logFileName := "log-" + currentDate + ".log"
	logFilePath := filepath.Join(tmpDir, logFileName)

	// 检查日志文件是否存在
	if _, err := os.Stat(logFilePath); os.IsNotExist(err) {
		t.Fatalf("Log file was not created: %s", logFilePath)
	}

	// 读取日志文件内容
	logContent, err := os.ReadFile(logFilePath)
	if err != nil {
		t.Fatalf("Failed to read log file: %s", err)
	}

	// 验证日志文件内容是否包含预期的日志条目
	if !strings.Contains(string(logContent), "Test log entry") {
		t.Errorf("Expected log entry not found in log file. Got: %s", logContent)
	}
}
