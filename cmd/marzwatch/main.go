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

const centralPort = "28443"

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
	fmt.Println("╭━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╮")
	fmt.Println("┃ 💠 CENTRAL CORE SETUP")
	fmt.Println("╰━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╯")
	fmt.Println("Port-e markazi be sorat sabet 28443 ast.")
	fmt.Println()

	name := ask(r, "Name server markazi", "Central-01")
	token := askRequired(r, "Telegram Bot Token")
	adminS := askRequired(r, "Telegram Admin Chat ID")
	admin, err := strconv.ParseInt(strings.TrimSpace(adminS), 10, 64)
	if err != nil {
		log.Fatal("Telegram Admin Chat ID dorost nist")
	}
	tz := ask(r, "Timezone report", "Asia/Tehran")

	ln, e := net.Listen("tcp", "0.0.0.0:"+centralPort)
	if e != nil {
		log.Fatalf("port %s azad nist: %v", centralPort, e)
	}
	_ = ln.Close()

	loc := geo.Detect()
	ip := loc.PublicIP
	if ip == "" {
		ip = askRequired(r, "Public IPv4 server markazi")
	}
	join, err := config.RandomToken(32)
	if err != nil {
		log.Fatal(err)
	}
	c := config.Config{
		Role:          "central",
		Name:          name,
		Listen:        "0.0.0.0:" + centralPort,
		PublicIP:      ip,
		Timezone:      tz,
		TelegramToken: token,
		AdminChatID:   admin,
		JoinToken:     join,
	}
	if err := config.Save("", c); err != nil {
		log.Fatal(err)
	}
	fmt.Println()
	fmt.Printf("✅ Central config sakhte shod • %s:%s\n", ip, centralPort)
}

func setupAgent() {
	r := bufio.NewReader(os.Stdin)
	fmt.Println("╭━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╮")
	fmt.Println("┃ 🛰 NODE LINK SETUP")
	fmt.Println("╰━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╯")
	fmt.Println("Faghat Name + Connection Token lazeme.")
	fmt.Println("IP va Port az dakhel Token khonde mishe.")
	fmt.Println()

	name := ask(r, "Node Name", "")
	key := askRequired(r, "Connection Token")
	ip, token, fp, err := config.ParseNodeToken(key)
	if err != nil {
		log.Fatal(err)
	}
	c := config.Config{
		Role:            "agent",
		Name:            name,
		CentralURL:      "https://" + ip + ":" + centralPort,
		JoinToken:       token,
		CertFingerprint: fp,
	}
	if err := config.Save("", c); err != nil {
		log.Fatal(err)
	}
	fmt.Println("✅ Node config sakhte shod")
	fmt.Printf("🔗 Central: %s:%s\n", ip, centralPort)
}

func showJoinKey() {
	c, err := config.Load("")
	if err != nil {
		log.Fatal(err)
	}
	if c.Role != "central" {
		log.Fatal("join-key faghat rooye Central kar mikone")
	}
	b, err := os.ReadFile("/var/lib/marzwatch/tls/server.crt")
	if err != nil {
		log.Fatal("TLS certificate hanooz amade nist; service ro start kon")
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		log.Fatal("invalid certificate")
	}
	cert, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("MW2@%s@%s@%s\n", c.PublicIP, c.JoinToken, security.Fingerprint(cert.Raw))
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
	fmt.Println("╭━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╮")
	fmt.Println("┃ 🗑 MARZWATCH CLEANUP")
	fmt.Println("╰━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╯")
	fmt.Println("Faghat file haye MarzWatch hazf mishan.")
	fmt.Println("Marzban / Xray / Docker / Firewall dast nemikhoran.")
	if strings.ToLower(ask(r, "Edame? [y/N]", "n")) != "y" {
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
	fmt.Println("✅ MarzWatch kamelan hazf shod. Service haye asli server dast nakhordan.")
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
