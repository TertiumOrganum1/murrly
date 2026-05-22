// Package paster sends a "paste" keystroke to the focused window.
package paster

type Paster struct{}

func New() *Paster { return &Paster{} }
