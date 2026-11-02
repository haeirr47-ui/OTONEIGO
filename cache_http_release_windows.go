//go:build windows && !p0debug

package main

import "net/http"

func cacheMediaHTTPClient() *http.Client { return nil }
