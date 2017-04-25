package logrus_bugsnag

import (
	"errors"
	"strings"

	"github.com/Sirupsen/logrus"
	"github.com/bugsnag/bugsnag-go"
	bugsnag_errors "github.com/bugsnag/bugsnag-go/errors"
)

type bugsnagHook struct{}

// ErrBugsnagUnconfigured is returned if NewBugsnagHook is called before
// bugsnag.Configure. Bugsnag must be configured before the hook.
var ErrBugsnagUnconfigured = errors.New("bugsnag must be configured before installing this logrus hook")

// ErrBugsnagSendFailed indicates that the hook failed to submit an error to
// bugsnag. The error was successfully generated, but `bugsnag.Notify()`
// failed.
type ErrBugsnagSendFailed struct {
	err error
}

func (e ErrBugsnagSendFailed) Error() string {
	return "failed to send error to Bugsnag: " + e.err.Error()
}

// NewBugsnagHook initializes a logrus hook which sends exceptions to an
// exception-tracking service compatible with the Bugsnag API. Before using
// this hook, you must call bugsnag.Configure(). The returned object should be
// registered with a log via `AddHook()`
//
// Entries that trigger an Error, Fatal or Panic should now include an "error"
// field to send to Bugsnag.
func NewBugsnagHook() (*bugsnagHook, error) {
	if bugsnag.Config.APIKey == "" {
		return nil, ErrBugsnagUnconfigured
	}
	return &bugsnagHook{}, nil
}

// Fire forwards an error to Bugsnag. Given a logrus.Entry, it extracts the
// "error" field (or the Message if the error isn't present) and sends it off.
func (hook *bugsnagHook) Fire(entry *logrus.Entry) error {
	var notifyErr error
	err, ok := entry.Data["error"].(error)
	if ok {
		notifyErr = err
	} else {
		notifyErr = errors.New(entry.Message)
	}

	metadata := bugsnag.MetaData{}
	metadata["metadata"] = make(map[string]interface{})
	for key, val := range entry.Data {
		if key != "error" {
			metadata["metadata"][key] = val
		}
	}

	skipStackFrames := calcSkipStackFrames(bugsnag_errors.New(notifyErr, 0))
	errWithStack := bugsnag_errors.New(notifyErr, skipStackFrames)
	bugsnagErr := bugsnag.Notify(errWithStack, metadata)
	if bugsnagErr != nil {
		return ErrBugsnagSendFailed{bugsnagErr}
	}

	return nil
}

// Levels enumerates the log levels on which the error should be forwarded to
// bugsnag: everything at or above the "Error" level.
func (hook *bugsnagHook) Levels() []logrus.Level {
	return []logrus.Level{
		logrus.ErrorLevel,
		logrus.FatalLevel,
		logrus.PanicLevel,
	}
}

const (
	logrusPkg        = "github.com/sirupsen/logrus"
	logrusBugsnagPkg = "github.com/shopify/logrus-bugsnag"
)

// calcSkipStackFrames calculates the offset to first stackframe that does
// not belong to logrus or logrus-bugsnag.
//
// We do this dynamically because calling log.WithFields().Error(),
// log.Error() and log.Errorf() generates different stracktrace lengths.
func calcSkipStackFrames(err *bugsnag_errors.Error) int {
	for i, stackFrame := range err.StackFrames() {
		stackFramePackage := strings.ToLower(stackFrame.Package)
		if stackFramePackage != logrusPkg && stackFramePackage != logrusBugsnagPkg {
			return i
		}
	}
	return 0
}
