package console

import (
	"fmt"
	"io"
)

// StatusPrinter writes state changes once.
type StatusPrinter struct {
	w    io.Writer
	last string
}

// NewStatusPrinter creates a status printer.
func NewStatusPrinter(w io.Writer) *StatusPrinter {
	return &StatusPrinter{w: w}
}

// Set writes a new status only when it changes.
func (p *StatusPrinter) Set(status string) {
	if p == nil || p.w == nil || status == "" || status == p.last {
		return
	}
	p.last = status
	fmt.Fprintln(p.w, status)
}
