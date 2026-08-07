package main

import (
	"bufio"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/SevinEW/marzban-monitoring/internal/agent"
	"github.com/SevinEW/marzban-monitoring/internal/central"
	"github.com/SevinEW/marzban-monitoring/internal/config"
	"github.com/SevinEW/marzban-monitoring/internal/geo"
	"github.com/SevinEW/marzban-monitoring/internal/security"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.LUTC)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		run()
	case "setup-central":
		mustRoot()
		setupCentral()
	case "setup-agent":
		mustRoot()
		setupAgent()
	case "join-key":
		showJoinKey()
	case "doctor":
		doctor()
	case "uninstall":
		mustRoot()
		uninstall()
	default:
		usage()
		os.Exit(2)
	}
}
func usage() {
	fmt.Println("MarzWatch\n\nCommands:\n  run\n  setup-central\n  setup-agent\n  join-key\n  doctor\n  uninstall")
}
func run() {
	c, err := config.Load("")
	if err != nil {
		log.Fatal(err)
	}
	if c.Role == "central" {
		log.Fatal(central.Run(c))
	} else {
		log.Fatal(agent.Run(c))
	}
}
func setupCentral() {
	r := bufio.NewReader(os.Stdin)
	fmt.Println("💠 MarzWatch Central Setup")
	name := ask(r, "Server name", "Central-01")
	token := askRequired(r, "Telegram Bot Token")
	adminS := askRequired(r, "Telegram Admin Chat ID")
	admin, err := strconv.ParseInt(strings.TrimSpace(adminS), 10, 64)
	if err != nil {
		log.Fatal("invalid admin id")
	}
	tz := ask(r, "Report timezone", "Asia/Tehran")
	port := ask(r, "Central port", "28443")
	ln, e := net.Listen("tcp", "0.0.0.0:"+port)
	if e != nil {
		log.Fatalf("port %s is not free: %v", port, e)
	}
	_ = ln.Close()
	loc := geo.Detect()
	ip := loc.PublicIP
	if ip == "" {
		ip = askRequired(r, "Public IPv4")
	}
	join, err := config.RandomToken(32)
	if err != nil {
		log.Fatal(err)
	}
	c := config.Config{Role: "central", Name: name, Listen: "0.0.0.0:" + port, PublicIP: ip, Timezone: tz, TelegramToken: token, AdminChatID: admin, JoinToken: join}
	if err := config.Save("", c); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("✅ Central config created for %s:%s\n", ip, port)
}
func setupAgent() {
	r := bufio.NewReader(os.Stdin)
	fmt.Println("🛰 MarzWatch Node Setup")
	ip := askRequired(r, "Central Server IP")
	port := ask(r, "Central port", "28443")
	key := askRequired(r, "Join Key")
	token, fp, err := config.ParseJoinKey(key)
	if err != nil {
		log.Fatal(err)
	}
	name := ask(r, "Node name", "")
	c := config.Config{Role: "agent", Name: name, CentralURL: "https://" + ip + ":" + port, JoinToken: token, CertFingerprint: fp}
	if err := config.Save("", c); err != nil {
		log.Fatal(err)
	}
	fmt.Println("✅ Node config created")
}
func showJoinKey() {
	c, err := config.Load("")
	if err != nil {
		log.Fatal(err)
	}
	if c.Role != "central" {
		log.Fatal("join-key is only available on central")
	}
	b, err := os.ReadFile("/var/lib/marzwatch/tls/server.crt")
	if err != nil {
		log.Fatal("central TLS certificate not ready; start service first")
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		log.Fatal("invalid certificate")
	}
	cert, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("MW1.%s.%s\n", c.JoinToken, security.Fingerprint(cert.Raw))
}
func doctor() {
	fmt.Println("╭━━━━━━━━━━━━━━━━━━━━━━╮\n┃ 🩺 MARZWATCH DOCTOR\n╰━━━━━━━━━━━━━━━━━━━━━━╯")
	c, err := config.Load("")
	if err != nil {
		fmt.Println("🔴 Config:", err)
		return
	}
	fmt.Println("🟢 Config OK")
	fmt.Println("🔵 Role:", c.Role)
	if c.Role == "central" {
		if _, err := os.Stat("/var/lib/marzwatch/tls/server.crt"); err == nil {
			fmt.Println("🟢 TLS certificate OK")
		} else {
			fmt.Println("🟡 TLS certificate not ready")
		}
	} else {
		if _, err := os.Stat("/var/lib/marzwatch/identity.json"); err == nil {
			fmt.Println("🟢 Node identity OK")
		} else {
			fmt.Println("🟡 Node not registered yet")
		}
	}
	cmd := exec.Command("systemctl", "is-active", "marzwatch")
	out, _ := cmd.Output()
	fmt.Println("🔵 Service:", strings.TrimSpace(string(out)))
	fmt.Println("🛡 Marzban/Xray config is not read or modified by Doctor.")
}
func uninstall() {
	r := bufio.NewReader(os.Stdin)
	fmt.Println("🗑 MarzWatch Uninstaller\nOnly MarzWatch files will be removed. Marzban/Xray/Docker/Firewall will NOT be changed.")
	if strings.ToLower(ask(r, "Continue? [y/N]", "n")) != "y" {
		fmt.Println("Cancelled")
		return
	}
	_ = exec.Command("systemctl", "disable", "--now", "marzwatch").Run()
	_ = os.Remove("/etc/systemd/system/marzwatch.service")
	_ = os.RemoveAll("/etc/marzwatch")
	_ = os.RemoveAll("/var/lib/marzwatch")
	_ = os.Remove("/usr/local/bin/marzwatchctl")
	_ = exec.Command("systemctl", "daemon-reload").Run()
	_ = exec.Command("userdel", "marzwatch").Run()
	_ = exec.Command("groupdel", "marzwatch").Run()
	self, _ := os.Executable()
	if filepath.Clean(self) == "/usr/local/bin/marzwatch" {
		_ = os.Remove(self)
	}
	fmt.Println("✅ MarzWatch removed. Existing server services were untouched.")
}
func ask(r *bufio.Reader, label, def string) string {
	if def != "" {
		fmt.Printf("%s [%s]: ", label, def)
	} else {
		fmt.Printf("%s: ", label)
	}
	s, _ := r.ReadString('\n')
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	return s
}
func askRequired(r *bufio.Reader, label string) string {
	for {
		s := ask(r, label, "")
		if s != "" {
			return s
		}
	}
}
func mustRoot() {
	if os.Geteuid() != 0 {
		log.Fatal("run as root")
	}
}
