package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/mattn/go-colorable"
	"github.com/sirupsen/logrus"
	"github.com/valyala/bytebufferpool"
)

// https://github.com/Mrs4s/go-cqhttp/blob/master/global/log_hook.go

// LogHook logrus本地钩子
type LogHook struct {
	lock      *sync.Mutex      // 锁
	levels    []logrus.Level   // hook级别
	formatter logrus.Formatter // 格式
	path      string           // 写入path
	writer    io.Writer        // io
}

var Hook *LogHook

// Levels ref: logrus/hooks.go impl Hook interface
func (hook *LogHook) Levels() []logrus.Level {
	if len(hook.levels) == 0 {
		return logrus.AllLevels
	}
	return hook.levels
}

// Fire ref: logrus/hooks.go impl Hook interface
func (hook *LogHook) Fire(entry *logrus.Entry) error {
	hook.lock.Lock()
	defer hook.lock.Unlock()

	if hook.writer != nil {
		return hook.ioWrite(entry)
	}

	if hook.path != "" {
		return hook.pathWrite(entry)
	}

	return nil
}

func (hook *LogHook) ioWrite(entry *logrus.Entry) error {
	log, err := hook.formatter.Format(entry)
	if err != nil {
		return err
	}

	_, err = hook.writer.Write(log)
	if err != nil {
		return err
	}
	return nil
}

func (hook *LogHook) pathWrite(entry *logrus.Entry) error {
	dir := filepath.Dir(hook.path)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return err
	}

	fd, err := os.OpenFile(hook.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o666)
	if err != nil {
		return err
	}
	defer fd.Close()

	log, err := hook.formatter.Format(entry)
	if err != nil {
		return err
	}

	_, err = fd.Write(log)
	return err
}

// NewLocalHook 初始化本地日志钩子实现
func NewLocalHook(args any, consoleFormatter LogFormat, fileFormatter logrus.Formatter, levels ...logrus.Level) *LogHook {
	hook := &LogHook{
		lock: new(sync.Mutex),
	}
	hook.SetFormatter(consoleFormatter, fileFormatter)
	hook.levels = append(hook.levels, levels...)

	switch arg := args.(type) {
	case string:
		hook.SetPath(arg)
	case io.Writer:
		hook.SetWriter(arg)
	default:
		panic(fmt.Sprintf("unsupported type: %v", reflect.TypeOf(args)))
	}

	return hook
}

// SetWriter set Writer
func (hook *LogHook) SetWriter(writer io.Writer) {
	hook.lock.Lock()
	defer hook.lock.Unlock()
	hook.writer = writer
}

// SetPath set log file draw
func (hook *LogHook) SetPath(path string) {
	hook.lock.Lock()
	defer hook.lock.Unlock()
	hook.path = path
}

// SetFormatter set log format
func (hook *LogHook) SetFormatter(consoleFormatter, fileFormatter logrus.Formatter) {
	hook.lock.Lock()
	defer hook.lock.Unlock()

	// 支持处理windows平台的console色彩
	logrus.SetOutput(colorable.NewColorableStdout())
	// 用于在console写出
	logrus.SetFormatter(consoleFormatter)
	// 用于写入文件
	hook.formatter = fileFormatter
}

func (hook *LogHook) ExecLogWrite(s string) error {
	fmt.Print(s)
	err := hook.ioWrite(&logrus.Entry{
		Level:   logrus.InfoLevel,
		Message: s,
	})
	if err != nil {
		return err
	}
	return nil
}

func (hook *LogHook) GetWriter() io.Writer {
	return hook.writer
}

const (
	colorCodePanic = "\x1b[1;31m" // color.Style{color.Bold, color.Red}.String()
	colorCodeFatal = "\x1b[1;31m" // color.Style{color.Bold, color.Red}.String()
	colorCodeError = "\x1b[31m"   // color.Style{color.Red}.String()
	colorCodeWarn  = "\x1b[33m"   // color.Style{color.Yellow}.String()
	colorCodeInfo  = "\x1b[37m"   // color.Style{color.White}.String()
	colorCodeDebug = "\x1b[32m"   // color.Style{color.Green}.String()
	colorCodeTrace = "\x1b[36m"   // color.Style{color.Cyan}.String()
	colorReset     = "\x1b[0m"
)

type LogFormat struct {
	Prefix      string // 日志前缀
	EnableColor bool
}

// Format implements logrus.Formatter
func (f LogFormat) Format(entry *logrus.Entry) ([]byte, error) {
	buf := bytebufferpool.Get()
	defer bytebufferpool.Put(buf)

	if f.EnableColor {
		_, _ = buf.WriteString(GetLogLevelColorCode(entry.Level))
	}

	if entry.Logger == nil {
		_, _ = buf.WriteString(entry.Message)
	} else {
		_, _ = buf.WriteString("[")
		_, _ = buf.WriteString(f.Prefix)
		_, _ = buf.WriteString("] [")
		_, _ = buf.WriteString(strings.ToUpper(entry.Level.String()))
		_, _ = buf.WriteString("] [")
		_, _ = buf.WriteString(entry.Time.Format("2006-01-02 15:04:05"))
		_, _ = buf.WriteString("]")
		if entry.Caller != nil {
			_, _ = buf.WriteString(fmt.Sprintf(" %s:%d",
				entry.Caller.File[strings.LastIndex(entry.Caller.File, "/")+1:],
				entry.Caller.Line))
		}
		_, _ = buf.WriteString(": ")
		_, _ = buf.WriteString(entry.Message)
		_, _ = buf.WriteString(" \n")
	}

	if f.EnableColor {
		_, _ = buf.WriteString(colorReset)
	}

	ret := make([]byte, len(buf.Bytes()))
	copy(ret, buf.Bytes()) // copy buffer
	return ret, nil
}

// GetLogLevel 获取日志等级
// "trace","debug","info","warn","warn","error"
func GetLogLevel(level string) []logrus.Level {
	switch level {
	case "trace":
		return []logrus.Level{
			logrus.TraceLevel, logrus.DebugLevel,
			logrus.InfoLevel, logrus.WarnLevel, logrus.ErrorLevel,
			logrus.FatalLevel, logrus.PanicLevel,
		}
	case "debug":
		return []logrus.Level{
			logrus.DebugLevel, logrus.InfoLevel,
			logrus.WarnLevel, logrus.ErrorLevel,
			logrus.FatalLevel, logrus.PanicLevel,
		}
	case "info":
		return []logrus.Level{
			logrus.InfoLevel, logrus.WarnLevel,
			logrus.ErrorLevel, logrus.FatalLevel, logrus.PanicLevel,
		}
	case "warn":
		return []logrus.Level{
			logrus.WarnLevel, logrus.ErrorLevel,
			logrus.FatalLevel, logrus.PanicLevel,
		}
	case "error":
		return []logrus.Level{
			logrus.ErrorLevel, logrus.FatalLevel,
			logrus.PanicLevel,
		}
	default:
		return []logrus.Level{
			logrus.InfoLevel, logrus.WarnLevel,
			logrus.ErrorLevel, logrus.FatalLevel, logrus.PanicLevel,
		}
	}
}

// GetLogLevelColorCode 获取日志等级对应色彩code
func GetLogLevelColorCode(level logrus.Level) string {
	switch level {
	case logrus.PanicLevel:
		return colorCodePanic
	case logrus.FatalLevel:
		return colorCodeFatal
	case logrus.ErrorLevel:
		return colorCodeError
	case logrus.WarnLevel:
		return colorCodeWarn
	case logrus.InfoLevel:
		return colorCodeInfo
	case logrus.DebugLevel:
		return colorCodeDebug
	case logrus.TraceLevel:
		return colorCodeTrace

	default:
		return colorCodeInfo
	}
}
