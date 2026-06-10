//go:build !linux

package a11ysetup

// Status mirrors the Linux shape; on other platforms nothing needs
// setting up (macOS covers this via the AX permission flow), so the
// stub always reports ready and the menu item stays hidden anyway.
type Status struct {
	ToolkitA11y bool
	VSCodeFound bool
	VSCode      bool
}

func (s Status) Ready() bool { return true }

func Check() Status { return Status{} }

func Apply() (Status, []string, error) { return Status{}, nil, nil }
