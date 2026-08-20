package main

import "log"

// handlePanic turns a panic in a background goroutine into a controlled exit
// that still hands the GPU buffers back.
//
// A panic in a goroutine other than the main one tears the process down
// without running main's defers, so the model would never be released — and
// dying with the ggml-CUDA backend still up is exactly what leaves the driver
// holding an allocation it does not reclaim. We log the trace first (so the
// crash is still diagnosable), then release, then exit with Go's own
// panic exit code.
//
// r is the recover() value: nil means the goroutine finished normally and this
// is a no-op. release and exit are parameters rather than direct calls to
// releaseModel/os.Exit so the behaviour is testable without killing the test
// binary.
func handlePanic(where string, r any, stack []byte, release func(), exit func(int)) {
	if r == nil {
		return
	}
	log.Printf("panic in %s: %v\n%s", where, r, stack)
	release()
	exit(panicExitCode)
}

// panicExitCode matches what the Go runtime uses for an unrecovered panic, so
// intercepting the crash does not change how anything watching the process
// reads its exit status.
const panicExitCode = 2
