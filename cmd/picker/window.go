package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// arrangeWindow places the picker the way a transient chooser should
// appear: out of the taskbar/pager, pinned above other windows, and
// centred on the monitor under the mouse — with no visible jump.
//
// The trick: the moment our window exists we make it fully transparent
// (_NET_WM_WINDOW_OPACITY = 0), so the WM's initial placement and our
// repositioning all happen while it's invisible; we only reveal it once
// it sits at the target. The window is found by OUR OWN pid (via
// _NET_WM_PID), so we can never touch another app's window. Best-effort
// and X11-only: each step shells out to a standard utility (xdotool /
// wmctrl / xprop / xrandr) and silently no-ops if one is missing.
// Run in a goroutine before ShowAndRun; it polls until the window exists.
func arrangeWindow() {
	mx, my, hasMouse := mouseLocation()
	pid := os.Getpid()
	dbg("arrange start: mouse=(%d,%d) ok=%v pid=%d", mx, my, hasMouse, pid)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		win := windowForPID(pid)
		if win == "" {
			time.Sleep(5 * time.Millisecond) // catch the window ASAP, before it paints
			continue
		}

		// Hide via the compositor before the window paints. This is a
		// direct X property, so it sticks even before the WM manages the
		// window.
		setOpacity(win, 0)

		// Crucial: let Fyne finish growing the window to its final scaled
		// size AND let the WM take it under management — all while it's
		// invisible. Doing flags/placement before this point gets silently
		// undone (the WM ignores state messages for unmanaged windows, and
		// Fyne re-centres the window after it finishes laying out).
		time.Sleep(280 * time.Millisecond)

		setWindowFlags(win) // now managed → skip_taskbar/pager + above stick
		if w, h, ok := windowSize(win); ok && hasMouse && w >= 200 && h >= 100 {
			if monX, monY, monW, monH, mok := monitorContaining(mx, my); mok {
				x := clamp(mx-w/2, monX, monX+monW-w)
				y := clamp(my-h/2, monY, monY+monH-h)
				dbg("size=(%d,%d) monitor=(%d,%d %dx%d) -> move (%d,%d)", w, h, monX, monY, monW, monH, x, y)
				moveWindow(win, x, y)
			}
		}
		setOpacity(win, 1) // reveal, now at the final position
		activateWindow(win)
		go watchFocusDismiss(pid)
		return
	}
	dbg("arrange: window never found within deadline")
}

// activateWindow gives the picker keyboard focus (so Esc works) and then
// re-pins skip_taskbar/above, since activating can re-list the window.
func activateWindow(win string) {
	exec.Command("xdotool", "windowactivate", "--sync", win).Run()
	setWindowFlags(win)
}

// watchFocusDismiss closes the picker as soon as keyboard focus leaves our
// process — i.e. the user clicked anywhere outside it. Matched by PID so a
// child/parent window id mismatch can't false-trigger. Waits until we've
// actually held focus once before arming.
func watchFocusDismiss(pid int) {
	seen := false
	for {
		time.Sleep(150 * time.Millisecond)
		fp := focusedPID()
		if fp == pid {
			seen = true
			continue
		}
		if seen && fp > 0 {
			os.Exit(1)
		}
	}
}

// focusedPID returns the PID owning the X11 window with input focus, or 0.
func focusedPID() int {
	out, err := exec.Command("xdotool", "getwindowfocus", "getwindowpid").Output()
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return pid
}

func dbg(format string, args ...any) {
	if os.Getenv("MURRLY_PICKER_DEBUG") == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "picker: "+format+"\n", args...)
}

func moveWindow(win string, x, y int) {
	err := exec.Command("xdotool", "windowmove", win, strconv.Itoa(x), strconv.Itoa(y)).Run()
	dbg("windowmove %s %d %d err=%v", win, x, y, err)
}

// setWindowFlags keeps the window out of the taskbar/pager and pins it
// above others. No windowactivate — activating was re-listing the window
// in the taskbar. wmctrl changes at most two state bits per call (hence
// two calls) and wants a hex id; xdotool hands us decimal.
func setWindowFlags(win string) {
	id, err := strconv.Atoi(strings.TrimSpace(win))
	if err != nil {
		return
	}
	hex := fmt.Sprintf("0x%08x", id)
	e1 := exec.Command("wmctrl", "-i", "-r", hex, "-b", "add,skip_taskbar,skip_pager").Run()
	e2 := exec.Command("wmctrl", "-i", "-r", hex, "-b", "add,above").Run()
	dbg("setWindowFlags %s skip=%v above=%v", hex, e1, e2)
}

// setOpacity sets _NET_WM_WINDOW_OPACITY (0 = transparent, 1 = opaque).
// The compositor (Muffin on Cinnamon) honours it; on a non-compositing
// setup this is a harmless no-op and the window just shows normally.
func setOpacity(win string, frac float64) {
	id, err := strconv.Atoi(strings.TrimSpace(win))
	if err != nil {
		return
	}
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	val := uint64(frac * 4294967295.0)
	e := exec.Command("xprop", "-id", fmt.Sprintf("0x%x", id),
		"-f", "_NET_WM_WINDOW_OPACITY", "32c",
		"-set", "_NET_WM_WINDOW_OPACITY", strconv.FormatUint(val, 10)).Run()
	dbg("setOpacity %#x -> %.2f err=%v", id, frac, e)
}

// windowForPID returns the (last) X11 window owned by pid, or "" if none
// has appeared yet. Relies on GLFW setting _NET_WM_PID, which it does.
func windowForPID(pid int) string {
	out, err := exec.Command("xdotool", "search", "--pid", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return ""
	}
	return ids[len(ids)-1]
}

// windowSize reads a window's outer pixel size via xdotool.
func windowSize(win string) (w, h int, ok bool) {
	out, err := exec.Command("xdotool", "getwindowgeometry", "--shell", win).Output()
	if err != nil {
		return 0, 0, false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if v, found := strings.CutPrefix(line, "WIDTH="); found {
			w, _ = strconv.Atoi(strings.TrimSpace(v))
		} else if v, found := strings.CutPrefix(line, "HEIGHT="); found {
			h, _ = strconv.Atoi(strings.TrimSpace(v))
		}
	}
	return w, h, w > 0 && h > 0
}

func mouseLocation() (x, y int, ok bool) {
	out, err := exec.Command("xdotool", "getmouselocation", "--shell").Output()
	if err != nil {
		return 0, 0, false
	}
	// Output: X=123\nY=456\nSCREEN=0\nWINDOW=...
	for _, line := range strings.Split(string(out), "\n") {
		if v, found := strings.CutPrefix(line, "X="); found {
			x, _ = strconv.Atoi(strings.TrimSpace(v))
		} else if v, found := strings.CutPrefix(line, "Y="); found {
			y, _ = strconv.Atoi(strings.TrimSpace(v))
		}
	}
	return x, y, true
}

var monitorRe = regexp.MustCompile(`(\d+)x(\d+)\+(\d+)\+(\d+)`)

// monitorContaining parses `xrandr --query` for the connected monitor
// whose rectangle contains (px,py).
func monitorContaining(px, py int) (x, y, w, h int, ok bool) {
	out, err := exec.Command("xrandr", "--query").Output()
	if err != nil {
		return 0, 0, 0, 0, false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, " connected") {
			continue
		}
		m := monitorRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		mw, _ := strconv.Atoi(m[1])
		mh, _ := strconv.Atoi(m[2])
		mx, _ := strconv.Atoi(m[3])
		my, _ := strconv.Atoi(m[4])
		if px >= mx && px < mx+mw && py >= my && py < my+mh {
			return mx, my, mw, mh, true
		}
	}
	return 0, 0, 0, 0, false
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
