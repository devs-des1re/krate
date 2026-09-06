package build

import (
	"bytes"
	"net/http"
)

type bytesBuffer struct{ b bytes.Buffer }

func (x *bytesBuffer) Write(p []byte) (int, error) { return x.b.Write(p) }
func (x *bytesBuffer) Bytes() []byte               { return x.b.Bytes() }
func (x *bytesBuffer) String() string              { return x.b.String() }

// headerMap converts an http.Header into a string map (first value per key).
func headerMap(h http.Header) map[string]string {
	m := make(map[string]string)
	for k, vs := range h {
		if len(vs) > 0 {
			m[k] = vs[0]
		}
	}
	return m
}

// headerToMap converts an http.Header into a string map.
func headerToMap(h http.Header) map[string]string { return headerMap(h) }

// copyHeaders copies every header from one http.Header to another.
func copyHeaders(dst, src http.Header) {
	for k, vs := range src {
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}
