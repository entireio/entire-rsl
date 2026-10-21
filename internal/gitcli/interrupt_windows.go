//go:build windows

package gitcli

import (
	"fmt"
	"os"
)

// interrupt stops git. Windows cannot deliver an interrupt to another
// process, so it is killed.
func interrupt(p *os.Process) error {
	if err := p.Kill(); err != nil {
		return fmt.Errorf("stop git: %w", err)
	}
	return nil
}
