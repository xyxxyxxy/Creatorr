//go:build !linux

package sponsorblock

import "os/exec"

func applyReencodeNice(cmd *exec.Cmd) {}
