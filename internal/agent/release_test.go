package agent

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCentralBundleOnlyPublishedAfterAllChecksumsPass(t *testing.T) {
	for _, bad := range []bool{false, true} {
		t.Run(fmt.Sprint(bad), func(t *testing.T) {
			dir := t.TempDir()
			bin := []byte("binary")
			script := []byte("updater")
			name := "marzwatch-linux-amd64"
			os.WriteFile(filepath.Join(dir, name), []byte("original"), 0600)
			sums := []byte(fmt.Sprintf("%x  %s\n%x  marzwatch-auto-update.sh\n", sha256.Sum256(bin), name, sha256.Sum256(script)))
			get := func(n string) ([]byte, error) {
				switch n {
				case "SHA256SUMS":
					return sums, nil
				case name:
					return bin, nil
				default:
					if bad {
						return []byte("tampered"), nil
					}
					return script, nil
				}
			}
			err := fetchReleaseBundle(dir, "amd64", get)
			if (err != nil) != bad {
				t.Fatal("checksum behavior", err)
			}
			result, _ := os.ReadFile(filepath.Join(dir, name))
			expected := "binary"
			if bad {
				expected = "original"
			}
			if string(result) != expected {
				t.Fatal("partial bundle replaced installation input")
			}
			entries, _ := filepath.Glob(filepath.Join(dir, ".central-release-*"))
			if len(entries) > 0 {
				t.Fatal("staging files retained")
			}
		})
	}
}
