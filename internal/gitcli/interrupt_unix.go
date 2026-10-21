//go:build !windows

package gitcli

import (
	"fmt"
	"os"
)

// interrupt asks git to stop, as the Entire CLI asks its plugins.
func interrupt(p *os.Process) error {
	if err := p.Signal(os.Interrupt); err != nil {
		return fmt.Errorf("interrupt git: %w", err)
	}
	return nil
}
