package crashreport

import "net/http"

// FatalHTTP is deferred at an owned HTTP boundary. ErrAbortHandler is a normal
// net/http stream abort, not an application crash; leave it to the HTTP server.
func FatalHTTP(component string) {
	if value := recover(); value != nil {
		if value == http.ErrAbortHandler {
			panic(http.ErrAbortHandler)
		}
		Terminate(component, value)
	}
}
