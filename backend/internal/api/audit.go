package api

import (
	"log"
	"net/http"
	"strings"
)

// audit logs a sensitive action (applying a manifest, opening a pod exec
// session, reading a Secret's decoded values) to the standard log, prefixed
// so it's easy to grep or ship to a log pipeline. This server has no user
// identity of its own to attribute the action to (see the README's Security
// model — it's either unauthenticated or behind a single shared
// AUTH_PASSWORD), so the request's source address is the best signal
// available; front it with a proxy that adds a real identity (e.g. an
// authenticating Ingress) if you need more than that.
func audit(r *http.Request, action string, fields ...string) {
	kv := ""
	for i := 0; i+1 < len(fields); i += 2 {
		// fields[i+1] often comes straight from a URL path segment (a resource
		// name, namespace, ...), which the user fully controls — strip newlines
		// so it can't forge extra fake log lines (CWE-117 log injection).
		kv += " " + fields[i] + "=" + sanitizeLogValue(fields[i+1])
	}
	log.Printf("AUDIT action=%s remote=%s%s", action, sanitizeLogValue(r.RemoteAddr), kv)
}

func sanitizeLogValue(s string) string {
	return strings.NewReplacer("\n", "\\n", "\r", "\\r").Replace(s)
}
