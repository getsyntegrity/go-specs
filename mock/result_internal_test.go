package mock

import (
	"strings"
	"testing"
)

func TestValueTypeMismatchPanics(t *testing.T) {
	r := Result{values: []any{"text", errString("boom")}}
	if Value[string](r, 0) != "text" {
		t.Fatal("matching type must be returned")
	}
	if r.Err(1) == nil || r.Err(1).Error() != "boom" {
		t.Fatal("Err must return the configured error")
	}
	for name, fn := range map[string]func(){
		"Value": func() { _ = Value[int](r, 0) },
		"Err":   func() { _ = r.Err(0) },
	} {
		func() {
			defer func() {
				msg, _ := recover().(string)
				if !strings.Contains(msg, "result 0") || !strings.Contains(msg, "string") {
					t.Fatalf("%s: unclear panic message %q", name, msg)
				}
			}()
			fn()
		}()
	}
}

type errString string

func (e errString) Error() string { return string(e) }
