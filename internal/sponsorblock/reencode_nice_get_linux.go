//go:build linux

package sponsorblock

import "golang.org/x/sys/unix"

func getReencodeNice(pid int) (int, error) {
	return unix.Getpriority(unix.PRIO_PROCESS, pid)
}
