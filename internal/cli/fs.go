package cli

import "os"

func readFile(path string) ([]byte, error) { return os.ReadFile(path) }

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func openAppend(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}
