package cli

import (
	"encoding/base64"
	"io"
)

func writeOSC52(out io.Writer, text string) {
	enc := base64.StdEncoding.EncodeToString([]byte(text))
	io.WriteString(out, "\x1b]52;c;"+enc+"\x07")
}
