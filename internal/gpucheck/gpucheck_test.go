package gpucheck

import (
	"fmt"
	"testing"
)

const mib = int64(1024 * 1024)

// The real shape of `nvidia-smi --query-gpu=uuid,name,memory.free,memory.total
// --format=csv,noheader,nounits` on the mixed pair this package was written
// for. Note the ordering trap it encodes: nvidia-smi lists the 3070 first
// (lower PCI bus), while CUDA's default fastest-first order makes the 4090
// its device 0.
const twoCards = "GPU-2644dfce-0719-4f7a-65f4-56c28f5c5d84, NVIDIA GeForce RTX 3070, 3582, 8192\n" +
	"GPU-723db0b4-29e3-73fc-135d-2fe337bdd00b, NVIDIA GeForce RTX 4090, 21766, 24564\n"

func TestParseDevices(t *testing.T) {
	devs, err := ParseDevices(twoCards)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(devs) != 2 {
		t.Fatalf("got %d devices, want 2", len(devs))
	}
	if devs[0].Name != "NVIDIA GeForce RTX 3070" {
		t.Errorf("first device name = %q", devs[0].Name)
	}
	if devs[0].UUID != "GPU-2644dfce-0719-4f7a-65f4-56c28f5c5d84" {
		t.Errorf("first device uuid = %q", devs[0].UUID)
	}
	if devs[1].FreeMiB != 21766 {
		t.Errorf("second device free = %d, want 21766", devs[1].FreeMiB)
	}
	if devs[1].TotalMiB != 24564 {
		t.Errorf("second device total = %d, want 24564", devs[1].TotalMiB)
	}
}

func TestParseDevicesRejectsMalformed(t *testing.T) {
	for _, out := range []string{
		"",
		"GPU-abc, NVIDIA GeForce RTX 3070, 3582\n",        // missing a field
		"GPU-abc, NVIDIA GeForce RTX 3070, [N/A], 8192\n", // free memory unavailable
		"GPU-abc, NVIDIA GeForce RTX 3070, 3582, [N/A]\n", // total memory unavailable
		"GPU-abc, NVIDIA GeForce RTX 3070, 1, 2, 3\n",     // extra field
	} {
		if _, err := ParseDevices(out); err == nil {
			t.Errorf("ParseDevices(%q) = nil error, want failure", out)
		}
	}
}

func TestSelectByName(t *testing.T) {
	devs, err := ParseDevices(twoCards)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name     string
		want     string
		found    bool
		wantName string
	}{
		{"the card we prefer by default", "3070", true, "NVIDIA GeForce RTX 3070"},
		{"case-insensitive substring", "rtx 4090", true, "NVIDIA GeForce RTX 4090"},
		{"card not installed", "5090", false, ""},
		{"no preference expressed", "", false, ""},
		{"whitespace is not a preference", "   ", false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, ok := SelectByName(devs, tc.want)
			if ok != tc.found {
				t.Fatalf("SelectByName(%q) found = %v, want %v", tc.want, ok, tc.found)
			}
			if ok && d.Name != tc.wantName {
				t.Errorf("SelectByName(%q) = %q, want %q", tc.want, d.Name, tc.wantName)
			}
		})
	}
}

func TestIsWeakest(t *testing.T) {
	for in, want := range map[string]bool{
		"weakest":   true,
		"  Weakest": true,
		"3070":      false,
		"":          false,
	} {
		if got := IsWeakest(in); got != want {
			t.Errorf("IsWeakest(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestSelectWeakest(t *testing.T) {
	// The machine this was written against: a 6 GB 1660 alongside a 24 GB
	// 4090, listed by nvidia-smi in bus order.
	const mixedPair = "GPU-1483bc1a-845a-8f62-2b33-098e1f8d323a, NVIDIA GeForce GTX 1660, %d, 6144\n" +
		"GPU-723db0b4-29e3-73fc-135d-2fe337bdd00b, NVIDIA GeForce RTX 4090, %d, 24564\n"

	tests := []struct {
		name               string
		free1660, free4090 int64
		model              int64
		wantName           string
	}{
		// The whole point of the setting: the big card stays free even
		// though it has far more room.
		{"small card has room to spare", 4000, 21766, 1549 * mib, "NVIDIA GeForce GTX 1660"},
		// A thin margin on the small card still beats moving to the big one:
		// the load may fail, but that is a load the user asked for.
		{"small card is tight, big card is roomy", 2118, 21766, 1549 * mib, "NVIDIA GeForce GTX 1660"},
		// Weights don't fit the small card at all — taking it would only mean
		// EnsureFree aborting the startup a moment later.
		{"weights don't fit the small card", 900, 21766, 1549 * mib, "NVIDIA GeForce RTX 4090"},
		// Nowhere to put it: pick the roomiest and let EnsureFree speak.
		{"no card has room", 900, 1000, 1549 * mib, "NVIDIA GeForce RTX 4090"},
		// Size unknown (model file unreadable) — nothing to weigh, smallest wins.
		{"unknown model size", 100, 21766, 0, "NVIDIA GeForce GTX 1660"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			devs, err := ParseDevices(fmt.Sprintf(mixedPair, tc.free1660, tc.free4090))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			d, why, ok := SelectWeakest(devs, tc.model)
			if !ok {
				t.Fatal("SelectWeakest found no device")
			}
			if d.Name != tc.wantName {
				t.Errorf("SelectWeakest = %q (%s), want %q", d.Name, why, tc.wantName)
			}
		})
	}

	if _, _, ok := SelectWeakest(nil, 1549*mib); ok {
		t.Error("SelectWeakest picked a device from an empty list")
	}
}

func TestFindByUUID(t *testing.T) {
	devs, err := ParseDevices(twoCards)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	d, ok := FindByUUID(devs, "GPU-723db0b4-29e3-73fc-135d-2fe337bdd00b")
	if !ok {
		t.Fatal("expected to find the 4090 by uuid")
	}
	if d.Name != "NVIDIA GeForce RTX 4090" {
		t.Errorf("got %q", d.Name)
	}

	if _, ok := FindByUUID(devs, "GPU-does-not-exist"); ok {
		t.Error("found a device that is not in the list")
	}
	if _, ok := FindByUUID(devs, ""); ok {
		t.Error("empty uuid matched a device")
	}
}

func TestIsUUID(t *testing.T) {
	tests := map[string]bool{
		"GPU-2644dfce-0719-4f7a-65f4-56c28f5c5d84": true,
		" GPU-abc": true,
		"0":        false,
		"1,0":      false,
		"":         false,
	}
	for in, want := range tests {
		if got := IsUUID(in); got != want {
			t.Errorf("IsUUID(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestDecide(t *testing.T) {
	tests := []struct {
		name    string
		freeMiB int64
		model   int64
		want    Verdict
	}{
		{"room to spare", 6000, 1549 * mib, VerdictOK},
		{"exactly headroom above weights", 1549 + headroomMiB, 1549 * mib, VerdictOK},
		{"one MiB short of headroom", 1549 + headroomMiB - 1, 1549 * mib, VerdictTight},
		{"weights fit, no headroom", 1549, 1549 * mib, VerdictTight},
		// The case that motivated the guard: large-v3-turbo weights are
		// 1549 MiB and an orphaned context left only 1273 MiB free.
		{"less free than weights", 1273, 1549 * mib, VerdictImpossible},
		{"nothing free", 0, 74 * mib, VerdictImpossible},
		// A tiny model on a busy card still fits: the guard must not stand in
		// the way just because the margin is small in absolute terms.
		{"tiny model, busy card", 1273, 74 * mib, VerdictOK},
		{"unknown model size", 100, 0, VerdictOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Decide(tc.freeMiB, tc.model); got != tc.want {
				t.Errorf("Decide(%d, %d) = %v, want %v", tc.freeMiB, tc.model, got, tc.want)
			}
		})
	}
}
