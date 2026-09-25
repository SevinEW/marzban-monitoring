package central

import (
	bytebuf "bytes"
	"encoding/json"
	"github.com/SevinEW/marzban-monitoring/internal/config"
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"github.com/SevinEW/marzban-monitoring/internal/storage"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegisterHTTPRetryReturnsSameIdentity(t *testing.T) {
	st, _ := storage.Open(filepath.Join(t.TempDir(), "state.json"))
	s := &Server{store: st, cfg: config.Config{JoinToken: "test", TelegramGroupID: 1}}
	q := model.RegisterRequest{Name: "one", Hostname: "template", ServerKey: strings.Repeat("a", 64)}
	body, _ := json.Marshal(q)
	var first model.RegisterResponse
	for i := 0; i < 3; i++ {
		r := httptest.NewRequest("POST", "/api/v1/register", bytebuf.NewReader(body))
		r.Header.Set("X-Marzwatch-Join", "test")
		w := httptest.NewRecorder()
		s.register(w, r)
		if w.Code != 200 {
			t.Fatalf("registration: %d", w.Code)
		}
		var got model.RegisterResponse
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = got
		} else if got != first {
			t.Fatal("registration retry created a new identity")
		}
	}
	if len(st.Nodes()) != 1 {
		t.Fatal("duplicate node")
	}
}

func TestCorruptForumStateCannotCreateBlankInstallation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "forum.json")
	if err := os.WriteFile(p, []byte(`{"nodes":`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readForumState(p); err == nil {
		t.Fatal("corrupt forum state accepted")
	}
}
