package sponsorblock

import (
	"os/exec"
	"runtime"
	"testing"
)

func TestReencodeNiceConstant(t *testing.T) {
	if reencodeNice != 10 {
		t.Fatalf("reencodeNice=%d want 10", reencodeNice)
	}
}

func TestApplyReencodeNiceNilSafe(t *testing.T) {
	applyReencodeNice(nil)
	applyReencodeNice(&exec.Cmd{})
}

func TestApplyReencodeNiceLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	applyReencodeNice(cmd)
	got, err := getReencodeNice(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if got != reencodeNice {
		t.Fatalf("nice=%d want %d", got, reencodeNice)
	}
}
