package assert

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestEqualValuesErrorsUsesErrorsIs(t *testing.T) {
	baseErr := errors.New("boom")
	wrapped := fmt.Errorf("wrap: %w", baseErr)
	if !EqualValues(t, baseErr, wrapped) {
		t.Fatal("expected errors.Is to detect wrapped error")
	}
}

func TestEqualValuesByteSlices(t *testing.T) {
	if !EqualValues(t, []byte("abc"), []byte("abc")) {
		t.Fatal("expected byte slices to be equal")
	}
	if EqualValues(t, []byte("abc"), []byte("abd")) {
		t.Fatal("expected byte slices to differ")
	}
}

func TestEqualValuesTimes(t *testing.T) {
	now := time.Now()
	if !EqualValues(t, now, now) {
		t.Fatal("expected identical times to be equal")
	}
}

func TestEqualValuesFallback(t *testing.T) {
	type sample struct {
		ID   int
		Data map[string]int
	}
	left := sample{ID: 1, Data: map[string]int{"a": 1}}
	right := sample{ID: 1, Data: map[string]int{"a": 1}}
	if !EqualValues(t, left, right) {
		t.Fatal("expected struct equality")
	}
}
