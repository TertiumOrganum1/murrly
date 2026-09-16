//go:build linux

package clipboard

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jezek/xgb/xproto"
)

// These run against the real X server on SECONDARY — not the clipboard, so a
// test run does not cost whoever started it whatever they had copied.
func testOwner(t *testing.T) *xOwner {
	t.Helper()
	if os.Getenv("DISPLAY") == "" {
		t.Skip("no X display")
	}
	o, err := selectionOwner()
	if err != nil {
		t.Skipf("no selection owner: %v", err)
	}
	return o
}

// ask converts the SECONDARY selection to a target the way any application
// does at paste time, and hands back the raw bytes.
func ask(t *testing.T, target string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "xclip", "-selection", "secondary", "-o", "-t", target).Output()
	if err != nil {
		t.Fatalf("converting SECONDARY to %s: %v", target, err)
	}
	return out
}

// The bug this whole owner exists for: xclip answered every target with its
// payload, so a requestor asking for TIMESTAMP got text where a number belongs
// and sat in its retry path for seconds. TIMESTAMP must come back as the 32-bit
// number we took the selection at, and must not be the text.
func TestOwnerAnswersTimestampWithANumber(t *testing.T) {
	o := testOwner(t)
	const text = "диктовка для проверки таймстампа"
	release, err := o.claim("secondary", text)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	defer release()

	// A property of type INTEGER is printed as a decimal number rather than
	// handed back as bytes — that it prints as a number at all is the point:
	// against the old xclip owner this came back as the dictation.
	got := strings.TrimSpace(string(ask(t, "TIMESTAMP")))
	ts, err := strconv.ParseUint(got, 10, 32)
	if err != nil {
		t.Fatalf("TIMESTAMP came back as %q, want a 32-bit number", got)
	}
	o.mu.Lock()
	want := o.held[xproto.AtomSecondary].stamp
	o.mu.Unlock()
	if xproto.Timestamp(ts) != want {
		t.Errorf("TIMESTAMP = %d, want %d", ts, want)
	}
	if strings.Contains(got, text) {
		t.Errorf("TIMESTAMP came back as the text itself")
	}
}

// TARGETS has to list what we can actually produce. An owner advertising two
// targets (which is all xclip ever offered) leaves requestors guessing.
func TestOwnerAdvertisesTheTargetsItServes(t *testing.T) {
	o := testOwner(t)
	release, err := o.claim("secondary", "текст")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	defer release()

	var got []string
	for _, line := range strings.Split(string(ask(t, "TARGETS")), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			got = append(got, line)
		}
	}
	for _, want := range ownedTargets {
		if !hasTarget(got, want) {
			t.Errorf("TARGETS = %v, missing %s", got, want)
		}
	}
}

// Every text flavour we advertise has to come back as the dictation, byte for
// byte — including the multi-byte characters that a wrong format would cut.
func TestOwnerServesEveryTextFlavour(t *testing.T) {
	o := testOwner(t)
	const text = "Проверка вставки — длинное предложение, с тире и запятыми."
	release, err := o.claim("secondary", text)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	defer release()

	for _, target := range []string{"UTF8_STRING", "text/plain;charset=utf-8", "text/plain", "STRING", "TEXT"} {
		if got := string(ask(t, target)); got != text {
			t.Errorf("%s = %q, want %q", target, got, text)
		}
	}
}

// A dictation longer than one ChangeProperty has to arrive whole: the reply is
// built by appending chunks before the requestor is told the property is ready.
func TestOwnerServesTextLongerThanOneChunk(t *testing.T) {
	o := testOwner(t)
	text := strings.Repeat("длинная диктовка ", 20000) // ~600 KB of UTF-8
	release, err := o.claim("secondary", text)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	defer release()

	if got := string(ask(t, "UTF8_STRING")); got != text {
		t.Errorf("got %d bytes back, want %d", len(got), len(text))
	}
}

// A target we cannot produce must be refused, so the requestor moves on to its
// next choice instead of parsing our text as a picture.
func TestOwnerRefusesTargetsItCannotProduce(t *testing.T) {
	o := testOwner(t)
	release, err := o.claim("secondary", "текст")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "xclip", "-selection", "secondary", "-o", "-t", "image/png").Output()
	if err == nil && len(out) > 0 {
		t.Errorf("image/png came back as %d bytes; an owner with no picture must refuse it", len(out))
	}
}

// Releasing must not pull a newer dictation out of the selection: the release
// func of a superseded claim belongs to text that is no longer there.
func TestReleasingASupersededClaimLeavesTheNewTextAlone(t *testing.T) {
	o := testOwner(t)
	stale, err := o.claim("secondary", "старая фраза")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	fresh, err := o.claim("secondary", "новая фраза")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	defer fresh()

	stale()
	if got := string(ask(t, "UTF8_STRING")); got != "новая фраза" {
		t.Errorf("after releasing the superseded claim the selection holds %q", got)
	}
}
