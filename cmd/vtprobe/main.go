// Command vtprobe runs a program inside an x/vt emulator, types some input,
// and dumps the emulator screen with the cursor marked. Spike tooling only.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/x/vt"
	cpty "github.com/creack/pty"
)

func main() {
	emu := vt.NewSafeEmulator(100, 30)
	cmd := exec.Command(os.Args[1], os.Args[2:]...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ptmx, err := cpty.StartWithSize(cmd, &cpty.Winsize{Cols: 100, Rows: 30})
	if err != nil {
		panic(err)
	}
	go io.Copy(ptmx, emu) // emulator replies -> child
	go io.Copy(emu, ptmx) // child output -> emulator
	time.Sleep(5 * time.Second)
	ptmx.WriteString("hello wor")
	time.Sleep(1500 * time.Millisecond)
	pos := emu.CursorPosition()
	fmt.Printf("cursor=(%d,%d) hidden=%v alt=%v\n", pos.X, pos.Y, emu.CursorHidden(), emu.IsAltScreen())
	for y, l := range strings.Split(emu.String(), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		mark := " "
		if y == pos.Y {
			mark = ">"
		}
		fmt.Printf("%s%2d|%s\n", mark, y, strings.TrimRight(l, " "))
	}
	// Exit cleanly so claude does not record a failed renderer start.
	ptmx.WriteString("\x15")
	time.Sleep(300 * time.Millisecond)
	ptmx.WriteString("/exit\r")
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		fmt.Println("claude did not exit; killing")
		cmd.Process.Kill()
	}
}
