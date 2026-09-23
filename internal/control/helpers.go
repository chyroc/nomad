package control

import (
	"net/http"
	"time"
)

func httpClientForLogin() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

func timestampName() string {
	return time.Now().Format("20060102150405")
}
