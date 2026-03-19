// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"net/http"
	"slices"
	"strings"
)

var connectHeaders = []string{
	"Content-Type",
	"Connect-Protocol-Version",
	"Connect-Timeout-Ms",
	"Grpc-Timeout",
}

// CORSMiddleware wraps next with CORS headers for the listed origins. If origins
// is empty the handler is returned unchanged (no CORS). Preflight OPTIONS
// requests receive a 204 response with Connect-compatible allow headers.
func CORSMiddleware(origins []string, next http.Handler) http.Handler {
	if len(origins) == 0 {
		return next
	}

	allowHeaders := strings.Join(connectHeaders, ", ")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" || !slices.Contains(origins, origin) {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", allowHeaders)
		w.Header().Set("Access-Control-Max-Age", "3600")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
