package spy_test

import "github.com/getsyntegrity/go-specs/mock"

// Notifier sends messages (e.g. email, log, event). Real code would implement this.
type Notifier interface {
	Notify(msg string)
}

// RealNotifier is a concrete implementation (e.g. sends to stdout or a queue).
type RealNotifier struct{}

func (n *RealNotifier) Notify(msg string) {
	// real implementation would send the message
	_ = msg
}

// SpyNotifier implements Notifier and records every Notify call into a Spy for tests.
type SpyNotifier struct {
	Spy *mock.Spy
}

func (n *SpyNotifier) Notify(msg string) {
	if n.Spy != nil {
		n.Spy.Call(msg)
	}
}
