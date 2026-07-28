//go:build !linux

package sponsorblock

import "fmt"

func getReencodeNice(pid int) (int, error) {
	return 0, fmt.Errorf("not linux")
}
