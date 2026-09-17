package transcriber

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

// cpuThreads is how many threads whisper.cpp gets when it runs on the
// processor. The binding's own answer is runtime.NumCPU(), and on an SMT
// machine that answer is actively harmful — ggml's matmul threads fight over
// the same physical cores and the run collapses.
//
// Measured on a Ryzen 9 5950X (16 cores / 32 threads), ggml-large-v3-turbo-q5_0,
// one full 30 s window:
//
//	32 threads (NumCPU) — 25.0 s
//	16 threads (cores)  —  5.9 s
//	 8 threads          —  8.9 s
//
// So: one thread per physical core. MURRLY_THREADS overrides it — that is the
// knob those numbers were produced with, and another CPU may well want a
// different point on that curve.
// CPUThreads is cpuThreads for the other engine in this app: it runs on the
// same processor, so the curve below is its curve too.
func CPUThreads() int { return cpuThreads() }

func cpuThreads() int {
	if v, err := strconv.Atoi(os.Getenv("MURRLY_THREADS")); err == nil && v > 0 {
		return v
	}
	if n := physicalCores(); n > 0 {
		return n
	}
	return runtime.NumCPU()
}

// physicalCores counts distinct physical cores from /proc/cpuinfo — the
// (physical id, core id) pairs, which is what collapses an SMT pair into the
// single core it really is. Returns 0 anywhere that file is not there (macOS,
// Windows), where the caller falls back to NumCPU.
func physicalCores() int {
	raw, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0
	}
	seen := map[string]struct{}{}
	var pkg, core string
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			// Blank line: end of one logical CPU's block.
			if pkg != "" && core != "" {
				seen[pkg+"/"+core] = struct{}{}
			}
			pkg, core = "", ""
			continue
		}
		switch strings.TrimSpace(key) {
		case "physical id":
			pkg = strings.TrimSpace(value)
		case "core id":
			core = strings.TrimSpace(value)
		}
	}
	if pkg != "" && core != "" {
		seen[pkg+"/"+core] = struct{}{}
	}
	return len(seen)
}
