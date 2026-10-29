package scripts

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

//go:embed recover_pasarguard_presence.py presence_source.py presence_fleet.py presence_auto.py presence_panel.py presence_core_stage.py node_remove.py
var presenceAssets embed.FS

// Install only fixed, release-embedded files. Receiving a plan can never install
// code. This entry point runs solely in the existing root updater/installer.
func InstallPresence() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("root required")
	}
	const dir = "/usr/local/lib/marzwatch"
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	entries, _ := presenceAssets.ReadDir(".")
	changed := false
	for _, entry := range entries {
		raw, _ := presenceAssets.ReadFile(entry.Name())
		path := filepath.Join(dir, entry.Name())
		old, _ := os.ReadFile(path)
		if bytes.Equal(old, raw) {
			continue
		}
		if err := os.WriteFile(path+".new", raw, 0750); err != nil {
			return err
		}
		if err := os.Rename(path+".new", path); err != nil {
			return err
		}
		changed = true
	}
	units := map[string]string{
 "marzwatch-uninstall.service": `[Unit]
Description=MarzWatch authenticated node removal
After=network-online.target
[Service]
Type=oneshot
ExecStart=/usr/bin/python3 /usr/local/lib/marzwatch/node_remove.py
TimeoutStartSec=150
Nice=15
CPUQuota=20%
MemoryMax=64M
TasksMax=32
UMask=0077
Environment=PYTHONDONTWRITEBYTECODE=1
`,
		"marzwatch-presence-enroll.service": `[Unit]
Description=MarzWatch verified fleet presence enrollment
After=network-online.target docker.service
[Service]
Type=oneshot
ExecStart=/usr/bin/python3 /usr/local/lib/marzwatch/presence_fleet.py
TimeoutStartSec=90
Nice=15
CPUQuota=20%
MemoryMax=128M
TasksMax=48
UMask=0077
Environment=PYTHONDONTWRITEBYTECODE=1
Environment=GOMAXPROCS=1
Environment=GOMEMLIMIT=32MiB
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/etc/marzwatch /var/lib/marzwatch-updater /run /tmp
PrivateTmp=true
`,
		"marzwatch-presence-enroll.timer": `[Unit]
Description=MarzWatch bounded presence recovery check
[Timer]
OnBootSec=45s
OnUnitInactiveSec=30s
RandomizedDelaySec=5s
Unit=marzwatch-presence-enroll.service
[Install]
WantedBy=timers.target
`}
	for name, raw := range units {
		path := filepath.Join("/etc/systemd/system", name)
		old, _ := os.ReadFile(path)
		if string(old) == raw {
			continue
		}
		if err := os.WriteFile(path+".new", []byte(raw), 0644); err != nil {
			return err
		}
		if err := os.Rename(path+".new", path); err != nil {
			return err
		}
		changed = true
	}
	if changed {
		if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
			return err
		}
	}
	if err := exec.Command("systemctl", "enable", "--now", "marzwatch-presence-enroll.timer").Run(); err != nil {
		return err
	}
	raw, _ := os.ReadFile("/etc/marzwatch/config.json")
	var cfg struct {
		Role string `json:"role"`
	}
	json.Unmarshal(raw, &cfg)
	if cfg.Role == "agent" {
		exec.Command("systemctl", "disable", "--now", "marzwatch-presence-recovery.timer").Run()
	}
	return nil
}
