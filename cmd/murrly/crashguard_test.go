package main

import "testing"

func TestHandlePanicReleasesAndExits(t *testing.T) {
	released := 0
	var exitCode = -1

	handlePanic("test loop", "boom", []byte("stack trace"), func() { released++ }, func(c int) { exitCode = c })

	if released != 1 {
		t.Errorf("release called %d times, want 1", released)
	}
	if exitCode != panicExitCode {
		t.Errorf("exit code %d, want %d", exitCode, panicExitCode)
	}
}

func TestHandlePanicIsNoOpWithoutPanic(t *testing.T) {
	released := 0
	exited := false

	handlePanic("test loop", nil, nil, func() { released++ }, func(int) { exited = true })

	if released != 0 {
		t.Errorf("release called %d times on a clean return, want 0", released)
	}
	if exited {
		t.Error("exit called on a clean return")
	}
}

// The guard exists to cover the real shape of the failure: a panic raised in a
// goroutine, where main's defers never run. Recovering it and routing it
// through handlePanic must both release and report.
func TestHandlePanicFromRecoveredGoroutinePanic(t *testing.T) {
	released := 0
	var exitCode = -1
	done := make(chan struct{})

	go func() {
		defer close(done)
		defer func() {
			handlePanic("app loop", recover(), []byte("stack"), func() { released++ }, func(c int) { exitCode = c })
		}()
		panic("inference blew up")
	}()
	<-done

	if released != 1 {
		t.Errorf("release called %d times, want 1", released)
	}
	if exitCode != panicExitCode {
		t.Errorf("exit code %d, want %d", exitCode, panicExitCode)
	}
}
