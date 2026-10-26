package presence

import (
	"fmt"
	"google.golang.org/protobuf/encoding/protowire"
	"testing"
)

func encoded(names ...string) []byte {
	var b []byte
	for _, n := range names {
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendString(b, n)
	}
	return b
}
func TestWireAndNamespace(t *testing.T) {
	raw := encoded("user>>>42>>>online", "user>>>42>>>online", "user>>>43>>>online")
	a, e := parseUsers(raw, "namespace-one")
	if e != nil || len(a) != 2 {
		t.Fatal("bulk parse failed")
	}
	b, e := parseUsers(raw, "namespace-two")
	if e != nil || a[0] == b[0] {
		t.Fatal("namespace collision")
	}
	for _, id := range a {
		if !hexID(id) {
			t.Fatal("raw identity exported")
		}
	}
	for _, raw := range [][]byte{encoded(""), encoded("42"), encoded("user>>>>>>online"), {10, 255}} {
		if _, e := parseUsers(raw, "x"); e == nil {
			t.Fatal("invalid wire accepted")
		}
	}
}
func TestBindingLoopbackAndAttestation(t *testing.T) {
	c := Binding{Schema: 1, Namespace: "0123456789abcdef", TrackingVerified: true, Cores: []Core{{Address: "127.0.0.1:11111", PID: 123, StartTicks: "456"}}}
	if ValidateBinding(c) != nil {
		t.Fatal("valid binding rejected")
	}
	c.Cores[0].Address = "0.0.0.0:11111"
	if ValidateBinding(c) == nil {
		t.Fatal("public API allowed")
	}
	c.Cores[0].Address = "127.0.0.1:11111"
	c.TrackingVerified = false
	if ValidateBinding(c) == nil {
		t.Fatal("unverified tracking allowed")
	}
}

func BenchmarkParseUsers8000(b *testing.B) {
	names := make([]string, 8000)
	for i := range names {
		names[i] = fmt.Sprintf("user>>>%d>>>online", i)
	}
	raw := encoded(names...)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, e := parseUsers(raw, "benchmark-panel-namespace"); e != nil {
			b.Fatal(e)
		}
	}
}
