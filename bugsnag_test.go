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
