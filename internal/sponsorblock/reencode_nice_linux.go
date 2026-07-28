//go:build linux

package sponsorblock

import (
	"os/exec"

	"golang.org/x/sys/unix"
)

// applyReencodeNice lowers scheduler priority of an already-started ffmpeg child.
func applyReencodeNice(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = unix.Setpriority(unix.PRIO_PROCESS, cmd.Process.Pid, reencodeNice)
}
