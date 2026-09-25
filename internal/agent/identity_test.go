package agent

import (
	"errors"
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOnlyUnknownNodeResetsIdentity(t *testing.T) {
	for _, body := range []string{"unknown node\n", "stale request\n", "bad signature\n", "unauthorized\n"} {
		t.Run(strings.TrimSpace(body), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401); _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			a := &Agent{client: server.Client(), id: identity{NodeID: "node", NodeSecret: "secret"}}
			a.cfg.CentralURL = server.URL
			err := a.sendMetric(model.Metric{})
			if err == nil || errors.Is(err, errIdentityRejected) != (body == "unknown node\n") {
				t.Fatalf("incorrect identity reset: %v", err)
			}
		})
	}
}

func TestFingerprintRejectsTemplatesAndIsStable(t *testing.T) {
	if machineFingerprint("", "") != "" || machineFingerprint(strings.Repeat("0", 32), "uuid") != "" {
		t.Fatal("template identity accepted")
	}
	machine := strings.Repeat("a", 32)
	a := machineFingerprint(machine, "550e8400-e29b-41d4-a716-446655440000")
	if len(a) != 64 || a != machineFingerprint(machine+"\n", "550E8400-E29B-41D4-A716-446655440000") {
		t.Fatal("unstable fingerprint")
	}
	if a == machineFingerprint(machine, "550e8400-e29b-41d4-a716-446655440001") {
		t.Fatal("different machines collided")
	}
}
