// Package gpucheck decides which GPU Whisper should load onto, and whether
// the model can plausibly fit there.
//
// Both jobs exist because CUDA and nvidia-smi do not agree on what "device 0"
// means. CUDA orders devices fastest-first by default, nvidia-smi orders them
// by PCI bus, so on a mixed pair the two numberings point at different cards
// and ggml silently lands on whichever one CUDA thinks is fastest. Everything
// here is therefore keyed on the device UUID, which is unambiguous under any
// ordering.
//
// The fit check is not a predictor of the real footprint — that depends on
// beam width and the compute buffers ggml allocates per context, neither of
// which we can know from here. It refuses only the arithmetically hopeless
// loads, before ggml starts allocating, because a cudaMalloc that fails
// part-way through a model load leaves the driver holding memory it never
// reclaims: nvidia-smi then shows VRAM charged to a PID that no longer
// exists, and only a reboot clears it. Refusing only what is certain means
// the check cannot block a startup that would have worked.
package gpucheck

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	// headroomMiB is what we would like free on top of the weights before
	// calling a load comfortable: the weights, plus a CUDA context (a couple
	// of hundred MiB on its own), plus per-context compute buffers that scale
	// with beam width. Thinner than this is worth a log line, not a refusal.
	headroomMiB = 512
	bytesPerMiB = 1024 * 1024
	// uuidPrefix is how CUDA and nvidia-smi both spell a device UUID.
	uuidPrefix = "GPU-"
	// WeakestName is the preferred_gpu value that means "whichever card is
	// the smallest", instead of naming a model. It exists because naming a
	// card ties the config to hardware that can be pulled out of the machine:
	// once the named card is gone the preference silently stops applying and
	// CUDA puts Whisper back on the fastest card — the exact outcome the
	// setting was there to avoid.
	WeakestName = "weakest"
)

// Device is one GPU as nvidia-smi reports it.
type Device struct {
	UUID     string
	Name     string
	FreeMiB  int64
	TotalMiB int64
}

// ParseDevices reads the output of
//
//	nvidia-smi --query-gpu=uuid,name,memory.free,memory.total --format=csv,noheader,nounits
//
// one device per line. Malformed lines are an error rather than a silent
// skip: a half-read device list would make the caller reason about the wrong
// card, which is the exact failure this package exists to prevent.
func ParseDevices(out string) ([]Device, error) {
	var devs []Device
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) != 4 {
			return nil, fmt.Errorf("unexpected nvidia-smi line %q", line)
		}
		free, err := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse free memory in %q: %w", line, err)
		}
		total, err := strconv.ParseInt(strings.TrimSpace(parts[3]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse total memory in %q: %w", line, err)
		}
		devs = append(devs, Device{
			UUID:     strings.TrimSpace(parts[0]),
			Name:     strings.TrimSpace(parts[1]),
			FreeMiB:  free,
			TotalMiB: total,
		})
	}
	if len(devs) == 0 {
		return nil, fmt.Errorf("no devices in nvidia-smi output")
	}
	return devs, nil
}

// SelectByName returns the first device whose name contains want, compared
// case-insensitively — so "3070" matches "NVIDIA GeForce RTX 3070". An empty
// want matches nothing: the caller asked for no preference.
func SelectByName(devs []Device, want string) (Device, bool) {
	want = strings.TrimSpace(want)
	if want == "" {
		return Device{}, false
	}
	want = strings.ToLower(want)
	for _, d := range devs {
		if strings.Contains(strings.ToLower(d.Name), want) {
			return d, true
		}
	}
	return Device{}, false
}

// IsWeakest reports whether a preferred_gpu value asks for the smallest card
// rather than naming one.
func IsWeakest(want string) bool {
	return strings.EqualFold(strings.TrimSpace(want), WeakestName)
}

// SelectWeakest picks the smallest card that can still hold modelBytes of
// weights, and explains the choice for the log.
//
// "Smallest" is total VRAM, not free VRAM: free memory swings with whatever
// else is on the screen right now, and a preference that flips between cards
// on every start is worse than no preference at all. Total capacity is what
// the user is actually expressing when they say "leave the big card alone".
//
// The fit ladder matters because the point of this setting is to keep the big
// card free, not to refuse to start: we take the smallest card with room to
// spare, else the smallest the weights merely fit on, else the card with the
// most free memory. That last rung can land on the big card — by then the
// alternative is EnsureFree aborting the startup, and a running Murrly on the
// wrong card beats no Murrly at all. A modelBytes of 0 (size unknown) means
// every card "fits" and the smallest one wins outright.
func SelectWeakest(devs []Device, modelBytes int64) (Device, string, bool) {
	if len(devs) == 0 {
		return Device{}, "", false
	}

	bySize := make([]Device, len(devs))
	copy(bySize, devs)
	sort.SliceStable(bySize, func(i, j int) bool { return bySize[i].TotalMiB < bySize[j].TotalMiB })

	for _, d := range bySize {
		if Decide(d.FreeMiB, modelBytes) == VerdictOK {
			return d, "smallest card with room to spare", true
		}
	}
	for _, d := range bySize {
		if Decide(d.FreeMiB, modelBytes) == VerdictTight {
			return d, "smallest card the weights fit on, with a thin margin", true
		}
	}

	roomiest := devs[0]
	for _, d := range devs[1:] {
		if d.FreeMiB > roomiest.FreeMiB {
			roomiest = d
		}
	}
	return roomiest, "no card has room for the weights — falling back to the one with the most free memory", true
}

// FindByUUID returns the device with the given UUID.
func FindByUUID(devs []Device, uuid string) (Device, bool) {
	uuid = strings.TrimSpace(uuid)
	if uuid == "" {
		return Device{}, false
	}
	for _, d := range devs {
		if d.UUID == uuid {
			return d, true
		}
	}
	return Device{}, false
}

// IsUUID reports whether a CUDA_VISIBLE_DEVICES value names a device by UUID
// rather than by index. Only the UUID form can be resolved to a specific card
// without knowing CUDA's device ordering, so it is the only form the fit
// check will act on.
func IsUUID(v string) bool {
	return strings.HasPrefix(strings.TrimSpace(v), uuidPrefix)
}

// Verdict classifies a prospective model load.
type Verdict int

const (
	// VerdictOK — the weights fit with room to spare.
	VerdictOK Verdict = iota
	// VerdictTight — the weights fit, but the margin above them is thinner
	// than headroomMiB. Worth logging, not worth refusing.
	VerdictTight
	// VerdictImpossible — less free memory than the weights alone occupy.
	VerdictImpossible
)

// Decide classifies a load of modelBytes worth of weights against freeMiB of
// free device memory. Callers that could not measure free memory must not
// call this at all — a failed probe should skip the check, not be encoded as
// a zero here, which legitimately means "no memory left" and is impossible.
func Decide(freeMiB, modelBytes int64) Verdict {
	if modelBytes <= 0 {
		return VerdictOK // nothing to weigh against
	}
	weightsMiB := modelBytes / bytesPerMiB
	switch {
	case freeMiB < weightsMiB:
		return VerdictImpossible
	case freeMiB < weightsMiB+headroomMiB:
		return VerdictTight
	default:
		return VerdictOK
	}
}

// WeightsMiB is the model file size in MiB, the unit the log messages and the
// Decide threshold are phrased in.
func WeightsMiB(modelBytes int64) int64 { return modelBytes / bytesPerMiB }

// Headroom is the comfortable-margin constant, exposed so callers can explain
// the numbers they report.
func Headroom() int64 { return headroomMiB }
