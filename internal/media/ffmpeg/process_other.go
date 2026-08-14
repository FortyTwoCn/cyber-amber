//go:build !linux && !windows

package ffmpeg

import (
	"os"
	"os/exec"
)

func configureProcess(_ *exec.Cmd) {}
func killProcessGroup(process *os.Process) error {
	if process == nil {
		return nil
	}
	return process.Kill()
}
