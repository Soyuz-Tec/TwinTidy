package diagnostics

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"
)

const appFolderName = "TwinTidy"

var (
	mu             sync.Mutex
	logger         = log.New(os.Stderr, "", log.LstdFlags|log.Lmicroseconds)
	logFile        *os.File
	logDirPath     string
	sessionLogPath string
)

var safeCrashFieldNames = map[string]struct{}{
	"files":           {},
	"folder_revision": {},
	"generation":      {},
	"group_count":     {},
	"groups":          {},
	"operation":       {},
	"phase":           {},
	"roots":           {},
	"selected_files":  {},
	"selected_count":  {},
	"worker":          {},
}

func Init(appName string) error {
	mu.Lock()
	defer mu.Unlock()

	if logFile != nil {
		return nil
	}

	if appName == "" {
		appName = appFolderName
	}

	dir, err := defaultLogDir(appName)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	path := filepath.Join(dir, fmt.Sprintf("session-%s-pid-%d.log", timestamp(), os.Getpid()))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}

	logFile = file
	logDirPath = dir
	sessionLogPath = path
	logger = log.New(file, "", log.LstdFlags|log.Lmicroseconds)
	log.SetOutput(file)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	logger.Printf("session started")
	return nil
}

func Close() {
	mu.Lock()
	defer mu.Unlock()

	if logFile == nil {
		return
	}
	logger.Printf("session ended")
	_ = logFile.Close()
	logFile = nil
}

func Logf(format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	logger.Printf(format, args...)
}

func LogDir() string {
	mu.Lock()
	defer mu.Unlock()
	return logDirPath
}

func SessionLogPath() string {
	mu.Lock()
	defer mu.Unlock()
	return sessionLogPath
}

// PanicToError converts a value obtained by calling recover directly inside a
// deferred function into a crash report and actionable error. Calling recover
// indirectly through a helper returns nil, so callers must pass the recovered
// value explicitly.
func PanicToError(scope string, recovered any, fields map[string]string) error {
	if recovered == nil {
		return nil
	}

	path := WriteCrashReport(scope, recovered, debug.Stack(), fields)
	return fmt.Errorf("unexpected internal error in %s; crash report saved to %s", scope, path)
}

func ReportPanicAndRepanic(scope string, fields map[string]string) {
	recovered := recover()
	if recovered == nil {
		return
	}

	WriteCrashReport(scope, recovered, debug.Stack(), fields)
	panic(recovered)
}

func WriteCrashReport(scope string, recovered any, stack []byte, fields map[string]string) string {
	mu.Lock()
	defer mu.Unlock()

	if logDirPath == "" {
		dir, err := defaultLogDir(appFolderName)
		if err == nil {
			_ = os.MkdirAll(dir, 0o755)
			logDirPath = dir
		} else {
			logDirPath = os.TempDir()
		}
	}

	path := filepath.Join(logDirPath, fmt.Sprintf("crash-%s-pid-%d.txt", timestamp(), os.Getpid()))
	report := buildCrashReport(scope, recovered, stack, fields)
	if err := os.WriteFile(path, []byte(report), 0o644); err != nil {
		logger.Printf("failed to write crash report: %v", err)
		return ""
	}

	logger.Printf("crash report written: %s", path)
	return path
}

func buildCrashReport(scope string, recovered any, stack []byte, fields map[string]string) string {
	exe, _ := os.Executable()
	cwd, _ := os.Getwd()

	var builder strings.Builder
	builder.WriteString("TwinTidy Crash Report\n")
	builder.WriteString("==================================\n\n")
	builder.WriteString("Time: ")
	builder.WriteString(time.Now().Format(time.RFC3339Nano))
	builder.WriteString("\nScope: ")
	builder.WriteString(scope)
	builder.WriteString("\nPanic: ")
	builder.WriteString(fmt.Sprint(recovered))
	builder.WriteString("\nGo: ")
	builder.WriteString(runtime.Version())
	builder.WriteString("\nOS/Arch: ")
	builder.WriteString(runtime.GOOS)
	builder.WriteString("/")
	builder.WriteString(runtime.GOARCH)
	builder.WriteString("\nExecutable: ")
	builder.WriteString(filepath.Base(exe))
	builder.WriteString("\nWorkingDir: ")
	builder.WriteString(redactedPathSummary(cwd))
	builder.WriteString("\nSessionLog: ")
	builder.WriteString(filepath.Base(sessionLogPath))
	builder.WriteString("\nArgs: ")
	builder.WriteString(argumentSummary(os.Args[1:]))
	builder.WriteString("\n\nFields:\n")

	if len(fields) == 0 {
		builder.WriteString("- none\n")
	} else {
		keys := make([]string, 0, len(fields))
		for key := range fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			builder.WriteString("- ")
			builder.WriteString(key)
			builder.WriteString(": ")
			builder.WriteString(crashFieldValue(key, fields[key]))
			builder.WriteString("\n")
		}
	}

	builder.WriteString("\nStack:\n")
	builder.Write(stack)
	if len(stack) == 0 || stack[len(stack)-1] != '\n' {
		builder.WriteString("\n")
	}
	return builder.String()
}

func crashFieldValue(key string, value string) string {
	if _, safe := safeCrashFieldNames[key]; safe {
		return value
	}
	return "[redacted]"
}

func argumentSummary(args []string) string {
	if len(args) == 0 {
		return "none"
	}

	summary := make([]string, 0, len(args))
	for _, argument := range args {
		if argument == "-h" || strings.HasPrefix(argument, "--") {
			summary = append(summary, argument)
		} else {
			summary = append(summary, "[redacted]")
		}
	}
	return strings.Join(summary, " ")
}

func redactedPathSummary(path string) string {
	if path == "" {
		return "unknown"
	}
	return "[redacted local path]"
}

func defaultLogDir(appName string) (string, error) {
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		return filepath.Join(localAppData, appName, "logs"), nil
	}

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, appName, "logs"), nil
}

func timestamp() string {
	return time.Now().Format("20060102-150405.000")
}
