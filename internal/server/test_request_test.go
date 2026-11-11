package server

import (
	"io"
	"net/http"
	"net/http/httptest"
)

func newTestRequest(method, target string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, target, body)
	if req.Host == "example.com" {
		req.Host = "localhost"
	}
	return req
}
