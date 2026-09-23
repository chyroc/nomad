package cli

import (
	"io"
	"os/exec"
)

func copyToClipboard(out io.Writer, text string) error {
	writeOSC52(out, text)
	for _, c := range [][]string{
		{"pbcopy"},
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
		{"xsel", "--clipboard", "--input"},
	} {
		if path, err := exec.LookPath(c[0]); err == nil {
			cmd := exec.Command(path, c[1:]...)
			pipe, err := cmd.StdinPipe()
			if err != nil {
				continue
			}
			if err := cmd.Start(); err != nil {
				continue
			}
			_, _ = io.WriteString(pipe, text)
			_ = pipe.Close()
			if err := cmd.Wait(); err == nil {
				return nil
			}
		}
	}
	return nil
}
