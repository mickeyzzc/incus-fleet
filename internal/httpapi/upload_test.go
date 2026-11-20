package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// deadlineRecorder records deadline adjustments made through the controller.
type deadlineRecorder struct {
	http.ResponseWriter
	readDeadline  time.Time
	writeDeadline time.Time
}

func (d *deadlineRecorder) SetReadDeadline(t time.Time) error {
	d.readDeadline = t
	return nil
}

func (d *deadlineRecorder) SetWriteDeadline(t time.Time) error {
	d.writeDeadline = t
	return nil
}

func TestStatusRecorderUnwraps(t *testing.T) {
	inner := &deadlineRecorder{ResponseWriter: httptest.NewRecorder()}
	rec := &statusRecorder{ResponseWriter: inner, status: http.StatusOK}

	// A controller must be able to walk through the wrapper to the inner writer;
	// before Unwrap existed this returned ErrNotSupported and long uploads were
	// cut off by the global server timeout.
	want := time.Now().Add(time.Minute)
	if err := http.NewResponseController(rec).SetWriteDeadline(want); err != nil {
		t.Fatalf("SetWriteDeadline through the wrapper = %v, want nil", err)
	}
	if inner.writeDeadline.IsZero() {
		t.Fatal("deadline did not reach the inner writer")
	}
}

func TestExtendUploadDeadlineRelaxesBothDeadlines(t *testing.T) {
	inner := &deadlineRecorder{ResponseWriter: httptest.NewRecorder()}
	rec := &statusRecorder{ResponseWriter: inner, status: http.StatusOK}

	before := time.Now()
	extendUploadDeadline(rec)

	if inner.readDeadline.IsZero() || inner.writeDeadline.IsZero() {
		t.Fatalf("deadlines not set: read=%v write=%v", inner.readDeadline, inner.writeDeadline)
	}
	if inner.readDeadline.Before(before.Add(time.Hour)) {
		t.Fatalf("read deadline %v is shorter than the upload budget", inner.readDeadline)
	}
	if inner.writeDeadline.Before(before.Add(time.Hour)) {
		t.Fatalf("write deadline %v is shorter than the upload budget", inner.writeDeadline)
	}
}
