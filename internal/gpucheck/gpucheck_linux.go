//go:build linux

package gpucheck

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// probeTimeout caps the nvidia-smi call. Both entry points sit on the startup
// path, so a wedged driver must not hold Murrly hostage — on timeout we carry
// on without the information.
const probeTimeout = 3 * time.Second

// envVisible is the variable CUDA reads to decide which devices exist. We set
// it to a UUID rather than an index because index order depends on
// CUDA_DEVICE_ORDER, which defaults to fastest-first and therefore does not
// match nvidia-smi's PCI-bus order on a mixed pair of cards.
const envVisible = "CUDA_VISIBLE_DEVICES"

// PreferDevice pins CUDA to the GPU the config asks for and returns its UUID.
// want is either the special value "weakest" — the smallest installed card
// that can still hold the model at modelPath — or a case-insensitive
// substring of a device name. It is a no-op — reported as false — when want is
// empty, when no installed GPU matches (the machine simply doesn't have that
// card, so CUDA's own default choice stands), when the device list can't be
// read, or when CUDA_VISIBLE_DEVICES is already set, which means the operator
// picked a device explicitly and we must not override them.
//
// modelPath is only consulted for the "weakest" form, and an unreadable one is
// not an error here: it just drops the fit half of the choice, leaving the
// smallest card. The loader reports a missing model far better than we can.
//
// Must be called before anything touches CUDA: the driver reads
// CUDA_VISIBLE_DEVICES once, at initialisation.
func PreferDevice(want, modelPath string) (string, bool) {
	if strings.TrimSpace(want) == "" {
		return "", false
	}
	if existing := os.Getenv(envVisible); existing != "" {
		log.Printf("gpucheck: %s already set to %q — leaving device choice alone", envVisible, existing)
		return "", false
	}

	devs, err := queryDevices()
	if err != nil {
		log.Printf("gpucheck: device preference skipped (%v)", err)
		return "", false
	}

	var (
		d  Device
		ok bool
	)
	if IsWeakest(want) {
		var why string
		d, why, ok = SelectWeakest(devs, modelSize(modelPath))
		if ok {
			log.Printf("gpucheck: %q among %s → %s (%s)", WeakestName, describe(devs), d.Name, why)
		}
	} else {
		d, ok = SelectByName(devs, want)
	}
	if !ok {
		log.Printf("gpucheck: no GPU matching %q among %s — using CUDA's default device", want, describe(devs))
		return "", false
	}
	if err := os.Setenv(envVisible, d.UUID); err != nil {
		log.Printf("gpucheck: could not set %s: %v", envVisible, err)
		return "", false
	}
	log.Printf("gpucheck: pinned to %s (%s)", d.Name, d.UUID)
	return d.UUID, true
}

// EnsureFree refuses a model load that cannot possibly fit in the free memory
// of the device CUDA will use, and logs a warning when it fits only barely.
// It returns an error ONLY for the impossible case; every other outcome —
// including every failure of the probe itself — is nil, because a broken or
// absent nvidia-smi must never be the reason Murrly won't start.
//
// The check runs only when CUDA_VISIBLE_DEVICES names a device by UUID, which
// is what PreferDevice sets. With an index, or with nothing set at all, we
// cannot tell which physical card CUDA will pick without knowing its device
// ordering — and checking the wrong card is worse than not checking.
func EnsureFree(modelPath string) error {
	fi, err := os.Stat(modelPath)
	if err != nil {
		return nil // the loader will report a missing model far better than we can
	}

	visible := os.Getenv(envVisible)
	if !IsUUID(visible) {
		log.Printf("gpucheck: VRAM check skipped — %s is %q, which does not identify a card unambiguously", envVisible, visible)
		return nil
	}

	devs, err := queryDevices()
	if err != nil {
		log.Printf("gpucheck: VRAM check skipped (%v)", err)
		return nil
	}
	d, ok := FindByUUID(devs, visible)
	if !ok {
		log.Printf("gpucheck: VRAM check skipped — %s is no longer present", visible)
		return nil
	}

	weights := WeightsMiB(fi.Size())
	switch Decide(d.FreeMiB, fi.Size()) {
	case VerdictImpossible:
		return fmt.Errorf("not enough free VRAM on %s: %d MiB free, model weights alone are %d MiB. "+
			"Free the card (or reboot if the memory is charged to a process that no longer exists) and start again",
			d.Name, d.FreeMiB, weights)
	case VerdictTight:
		log.Printf("gpucheck: %s has %d MiB free for %d MiB of weights — under the %d MiB we'd like on top for the CUDA context and compute buffers; the load may still fail",
			d.Name, d.FreeMiB, weights, Headroom())
	default:
		log.Printf("gpucheck: %s has %d MiB free for %d MiB of weights", d.Name, d.FreeMiB, weights)
	}
	return nil
}

// queryDevices asks nvidia-smi for the installed GPUs and their free memory.
func queryDevices() ([]Device, error) {
	bin, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return nil, fmt.Errorf("nvidia-smi not found")
	}

	cmd := exec.Command(bin, "--query-gpu=uuid,name,memory.free,memory.total", "--format=csv,noheader,nounits")
	done := make(chan struct{})
	timer := time.AfterFunc(probeTimeout, func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		close(done)
	})
	out, err := cmd.Output()
	if !timer.Stop() {
		<-done
		return nil, fmt.Errorf("nvidia-smi timed out after %s", probeTimeout)
	}
	if err != nil {
		return nil, fmt.Errorf("nvidia-smi: %w", err)
	}
	return ParseDevices(strings.TrimSpace(string(out)))
}

// modelSize is the weight file's size in bytes, or 0 when it can't be read —
// the value Decide reads as "unknown, don't weigh anything against it".
func modelSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// describe renders the installed GPUs for a log line explaining a miss.
func describe(devs []Device) string {
	names := make([]string, 0, len(devs))
	for _, d := range devs {
		names = append(names, d.Name)
	}
	return strings.Join(names, ", ")
}
