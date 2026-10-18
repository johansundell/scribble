package logging

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
)

type recorder struct{ n int }

func (r *recorder) Infof(string, ...interface{})    { r.n++ }
func (r *recorder) Warningf(string, ...interface{}) { r.n++ }
func (r *recorder) Errorf(string, ...interface{})   { r.n++ }

func TestOrStd(t *testing.T) {
	if _, ok := OrStd(nil).(Std); !ok {
		t.Error("expected Std for a nil logger")
	}
	r := &recorder{}
	OrStd(r).Infof("x")
	if r.n != 1 {
		t.Error("expected the given logger to be used")
	}
}

func TestStd(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	s := Std{}

	s.Infof("info %s", "msg")
	if !strings.Contains(buf.String(), "info msg") {
		t.Errorf("expected 'info msg', got %q", buf.String())
	}
	buf.Reset()

	s.Warningf("warning %s", "msg")
	if !strings.Contains(buf.String(), "WARNING: warning msg") {
		t.Errorf("expected 'WARNING: warning msg', got %q", buf.String())
	}
	buf.Reset()

	s.Errorf("error %s", "msg")
	if !strings.Contains(buf.String(), "ERROR: error msg") {
		t.Errorf("expected 'ERROR: error msg', got %q", buf.String())
	}
}
