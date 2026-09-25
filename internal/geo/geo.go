package geo

import (
	"net/http"
	"time"
)

// Ignore proxy environment variables when resolving the server address.
var client = &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}}
