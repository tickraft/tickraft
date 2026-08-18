// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package middleware

import (
	"bytes"
	"compress/gzip"
	"context"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
)

// MinCompressSize is the minimum response body size in bytes that will be
// compressed. Bodies smaller than this are passed through unchanged because
// the gzip overhead would outweigh the savings.
const MinCompressSize = 1024

// compressibleContentTypes lists content-type prefixes eligible for gzip
// compression. Binary formats (images, video, already-compressed archives)
// are excluded because gzip would not reduce their size.
var compressibleContentTypes = []string{
	"text/",
	"application/json",
	"application/javascript",
	"application/xml",
	"application/xhtml+xml",
	"application/ld+json",
	"image/svg+xml",
}

// Gzip returns a Hertz middleware that compresses response bodies with gzip
// when the client advertises gzip support via the Accept-Encoding request
// header and the response content type is compressible.
//
// The middleware runs after downstream handlers (post-compression): it
// inspects the fully-buffered response body and replaces it with the
// gzip-compressed bytes when beneficial. Responses smaller than
// MinCompressSize, responses that already carry a Content-Encoding header,
// and responses with non-compressible content types are passed through.
//
// Compatibility: only operates on buffered responses. Streaming responses
// (SetBodyStream) are not compressed to avoid buffering large payloads.
func Gzip() app.HandlerFunc {
	return func(ctx context.Context, arc *app.RequestContext) {
		// Skip entirely when the client does not accept gzip.
		if !acceptsGzip(arc) {
			arc.Next(ctx)
			return
		}

		arc.Next(ctx)

		// Skip if the response is already encoded (e.g. by a downstream proxy).
		if len(arc.Response.Header.Peek("Content-Encoding")) > 0 {
			return
		}

		// Skip status codes that never carry a body worth compressing.
		status := arc.Response.StatusCode()
		if status < 200 || status == 204 || status == 304 {
			return
		}

		// Skip streaming responses — buffering them would defeat the purpose.
		if arc.Response.IsBodyStream() {
			return
		}

		// Only compress known-compressible content types.
		contentType := string(arc.Response.Header.ContentType())
		if !isCompressibleContentType(contentType) {
			return
		}

		body := arc.Response.Body()
		if len(body) < MinCompressSize {
			return
		}

		compressed, ok := gzipBytes(body)
		if !ok {
			return
		}

		arc.Response.SetBody(compressed)
		arc.Response.Header.Set("Content-Encoding", "gzip")
		// Vary tells caches that the response representation depends on the
		// Accept-Encoding request header, preventing serving a gzipped
		// response to a client that does not accept gzip.
		arc.Response.Header.Set("Vary", "Accept-Encoding")
	}
}

// acceptsGzip reports whether the request's Accept-Encoding header allows
// gzip. Hertz parses header values as byte slices, so we convert once.
func acceptsGzip(arc *app.RequestContext) bool {
	acceptEncoding := string(arc.GetHeader("Accept-Encoding"))
	if acceptEncoding == "" {
		return false
	}
	for part := range strings.SplitSeq(acceptEncoding, ",") {
		encoding := strings.TrimSpace(part)
		// Strip q-value (e.g. "gzip;q=0.8").
		if idx := strings.Index(encoding, ";"); idx >= 0 {
			encoding = strings.TrimSpace(encoding[:idx])
		}
		if strings.EqualFold(encoding, "gzip") {
			return true
		}
	}
	return false
}

// isCompressibleContentType reports whether the given Content-Type value
// refers to a textual or otherwise compressible payload. The check is
// case-insensitive and ignores parameters (e.g. "; charset=utf-8").
func isCompressibleContentType(contentType string) bool {
	if contentType == "" {
		return false
	}
	contentType = strings.ToLower(contentType)
	if idx := strings.Index(contentType, ";"); idx >= 0 {
		contentType = strings.TrimSpace(contentType[:idx])
	}
	for _, prefix := range compressibleContentTypes {
		if strings.HasPrefix(contentType, prefix) {
			return true
		}
	}
	return false
}

// gzipBytes compresses src using gzip at the default compression level and
// returns the compressed bytes. The boolean return is false when compression
// failed or when the compressed output is not smaller than the input.
func gzipBytes(src []byte) ([]byte, bool) {
	var buf bytes.Buffer
	// DefaultCompression (-1) maps to level 6, a good speed/ratio trade-off.
	w, err := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
	if err != nil {
		return nil, false
	}
	if _, err = w.Write(src); err != nil {
		_ = w.Close() // best-effort close after write error, error not actionable
		return nil, false
	}
	if err = w.Close(); err != nil {
		return nil, false
	}
	// Reject if compression did not reduce the size — rare for textual
	// payloads above MinCompressSize, but possible for highly random input.
	if buf.Len() >= len(src) {
		return nil, false
	}
	return buf.Bytes(), true
}
