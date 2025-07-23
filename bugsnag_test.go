package logrus_bugsnag

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/bugsnag/bugsnag-go/v2"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// Copied from bugsnag tests

var roundTripper = &nilRoundTripper{}
var events = make(chan *bugsnag.Event, 10)
var testAPIKey = "12345678901234567890123456789012"
var errTest = errors.New("test error")

type nilRoundTripper struct{}

func (rt *nilRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		Body:       io.NopCloser(bytes.NewReader(nil)),
		StatusCode: http.StatusOK,
	}, nil
}

func init() {
	l := logrus.New()
	l.Out = io.Discard

	bugsnag.Configure(bugsnag.Configuration{
		APIKey: testAPIKey,
		Endpoints: bugsnag.Endpoints{
			Notify: "",
		},
		Synchronous:         true,
		Transport:           roundTripper,
		Logger:              l,
		AutoCaptureSessions: false,
	})
	//nolint:revive // config parameter is required by bugsnag API
	bugsnag.OnBeforeNotify(func(event *bugsnag.Event, config *bugsnag.Configuration) error {
		events <- event
		return nil
	})
}

func TestNewBugsnagHook(t *testing.T) {
	l := logrus.New()
	l.Out = io.Discard

	hook, err := NewBugsnagHook()
	assert.NoError(t, err)
	l.Hooks.Add(hook)

	t.Run("inline error", func(t *testing.T) {
		t.Run("inline logging", func(t *testing.T) {
			l.WithError(err).Error(errors.New("foo"))

			event := readEvent(t)
			assert.Equal(t, "*errors.errorString", event.ErrorClass)
			assert.Equal(t, "foo", event.Message)
			assert.NotEqual(t, "triggerError", event.Stacktrace[0].Method)
			assert.Contains(t, event.Stacktrace[0].File, "bugsnag_test.go")
		})

		t.Run("other function logging", func(t *testing.T) {
			triggerError(l, errors.New("foo"))

			event := readEvent(t)
			assert.Equal(t, "*errors.errorString", event.ErrorClass)
			assert.Equal(t, "foo", event.Message)
			assert.Equal(t, "triggerError", event.Stacktrace[0].Method)
		})
	})

	t.Run("prebuilt error", func(t *testing.T) {
		t.Run("inline logging", func(t *testing.T) {
			l.WithError(errTest).WithField("foo", "bar").Error("test")

			event := readEvent(t)
			assert.Equal(t, "*errors.errorString", event.ErrorClass)
			assert.Equal(t, "test error", event.Message)
			assert.Equal(t, "bar", event.MetaData["metadata"]["foo"])
			assert.NotEqual(t, "triggerError", event.Stacktrace[0].Method)
			assert.Contains(t, event.Stacktrace[0].File, "bugsnag_test.go")
		})

		t.Run("other function logging", func(t *testing.T) {
			triggerError(l, errTest)

			event := readEvent(t)
			assert.Equal(t, "*errors.errorString", event.ErrorClass)
			assert.Equal(t, "test error", event.Message)
			assert.Equal(t, "bar", event.MetaData["metadata"]["foo"])
			assert.Equal(t, "triggerError", event.Stacktrace[0].Method)
		})
	})

	t.Run("panic", func(t *testing.T) {
		t.Run("log panic", func(t *testing.T) {
			func() {
				defer func() {
					_ = recover()
				}()

				l.WithField("foo", "bar").Panic("test panic")
			}()

			event := readEvent(t)
			assert.Equal(t, "*errors.errorString", event.ErrorClass)
			assert.Equal(t, "test panic", event.Message)
			assert.Equal(t, "bar", event.MetaData["metadata"]["foo"])
			assert.NotEqual(t, "triggerError", event.Stacktrace[0].Method)
			assert.Contains(t, event.Stacktrace[0].File, "bugsnag_test.go")
		})

		t.Run("other function panic", func(t *testing.T) {
			func() {
				defer func() {
					_ = recover()
				}()

				triggerPanic(l, "test panic")
			}()

			event := readEvent(t)
			assert.Equal(t, "*errors.errorString", event.ErrorClass)
			assert.Equal(t, "test panic", event.Message)
			assert.Equal(t, "bar", event.MetaData["metadata"]["foo"])
			assert.Equal(t, "triggerPanic", event.Stacktrace[0].Method)
		})
	})
}

func triggerError(l *logrus.Logger, err error) {
	l.WithError(err).WithField("foo", "bar").Error("test")
}

func triggerPanic(l *logrus.Logger, msg string) {
	l.WithField("foo", "bar").Panic(msg)
}

func TestNewBugsnagHookWithMinLevel(t *testing.T) {
	l := logrus.New()
	l.Out = io.Discard

	// Create variables for levels so we can take their addresses
	infoLevel := logrus.InfoLevel
	errorLevel := logrus.ErrorLevel

	testCases := []struct {
		name              string
		minLevel          *logrus.Level
		testLevel         logrus.Level
		shouldTrigger     bool
		expectedLevelsLen int
	}{
		{
			name:              "no min level - error should trigger",
			minLevel:          nil,
			testLevel:         logrus.ErrorLevel,
			shouldTrigger:     true,
			expectedLevelsLen: 0, // levels should be nil
		},
		{
			name:              "no min level - info should not trigger",
			minLevel:          nil,
			testLevel:         logrus.InfoLevel,
			shouldTrigger:     false,
			expectedLevelsLen: 0, // levels should be nil
		},
		{
			name:              "min level info - info should trigger",
			minLevel:          &infoLevel,
			testLevel:         logrus.InfoLevel,
			shouldTrigger:     true,
			expectedLevelsLen: 5, // panic, fatal, error, warn, info
		},
		{
			name:              "min level info - error should trigger",
			minLevel:          &infoLevel,
			testLevel:         logrus.ErrorLevel,
			shouldTrigger:     true,
			expectedLevelsLen: 5, // panic, fatal, error, warn, info
		},
		{
			name:              "min level info - debug should not trigger",
			minLevel:          &infoLevel,
			testLevel:         logrus.DebugLevel,
			shouldTrigger:     false,
			expectedLevelsLen: 5, // panic, fatal, error, warn, info
		},
		{
			name:              "min level error - error should trigger",
			minLevel:          &errorLevel,
			testLevel:         logrus.ErrorLevel,
			shouldTrigger:     true,
			expectedLevelsLen: 3, // panic, fatal, error
		},
		{
			name:              "min level error - warn should not trigger",
			minLevel:          &errorLevel,
			testLevel:         logrus.WarnLevel,
			shouldTrigger:     false,
			expectedLevelsLen: 3, // panic, fatal, error
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Clear any existing events
			for len(events) > 0 {
				<-events
			}

			var hook *Hook
			var err error

			if tc.minLevel == nil {
				hook, err = NewBugsnagHook()
			} else {
				hook, err = NewBugsnagHook(*tc.minLevel)
			}

			assert.NoError(t, err)

			// Check that levels are set correctly
			if tc.minLevel == nil {
				assert.Nil(t, hook.levels)
			} else {
				assert.Len(t, hook.levels, tc.expectedLevelsLen)
				// Verify the hook's Levels() method returns the expected levels
				hookLevels := hook.Levels()
				containsTestLevel := false
				for _, level := range hookLevels {
					if level == tc.testLevel {
						containsTestLevel = true
						break
					}
				}
				assert.Equal(t, tc.shouldTrigger, containsTestLevel)
			}

			// Test if the hook fires for the given level
			logger := logrus.New()
			logger.Out = io.Discard
			logger.Hooks.Add(hook)

			// Log at the test level
			switch tc.testLevel {
			case logrus.PanicLevel:
				func() {
					defer func() { _ = recover() }()
					logger.WithError(errTest).Panic("test panic")
				}()
			case logrus.FatalLevel:
				// Note: We can't actually test Fatal because it calls os.Exit
				// So we'll skip this case in the test
				return
			case logrus.ErrorLevel:
				logger.WithError(errTest).Error("test error")
			case logrus.WarnLevel:
				logger.WithError(errTest).Warn("test warn")
			case logrus.InfoLevel:
				logger.WithError(errTest).Info("test info")
			case logrus.DebugLevel:
				logger.WithError(errTest).Debug("test debug")
			case logrus.TraceLevel:
				logger.WithError(errTest).Trace("test trace")
			}

			// Check if event was received
			select {
			case event := <-events:
				if !tc.shouldTrigger {
					t.Errorf("Expected no event, but received: %v", event)
				}
				// If we expected an event, verify it's correct
				assert.Equal(t, "*errors.errorString", event.ErrorClass)
				assert.Equal(t, "test error", event.Message)
			case <-time.After(100 * time.Millisecond):
				if tc.shouldTrigger {
					t.Error("Expected event but none received")
				}
			}
		})
	}
}

func readEvent(t *testing.T) *bugsnag.Event {
	timer := time.NewTimer(1 * time.Second)
	defer timer.Stop()

	select {
	case <-timer.C:
		t.Error("timeout waiting for event")
		return nil
	case e := <-events:
		return e
	}
}
