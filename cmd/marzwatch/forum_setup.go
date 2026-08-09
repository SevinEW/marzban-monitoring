package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/SevinEW/marzban-monitoring/internal/config"
	"github.com/SevinEW/marzban-monitoring/internal/telegram"
)

func forumSetup() {
	c, err := config.Load("")
	if err != nil {
		log.Fatal(err)
	}
	if c.Role != "central" {
		log.Fatal("forum-setup faghat rooye Central kar mikone")
	}

	r := bufio.NewReader(os.Stdin)
	fmt.Println("╭━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╮")
	fmt.Println("┃ 💠 MARZWATCH FORUM SETUP")
	fmt.Println("╰━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━╯")
	fmt.Println("Group bayad Supergroup bashe va Topics roshan bashan.")
	fmt.Println("Bot bayad Admin ba Manage Topics + Delete Messages bashe.")
	fmt.Println()

	gidText := askRequired(r, "Telegram Forum Group ID")
	gid, err := strconv.ParseInt(strings.TrimSpace(gidText), 10, 64)
	if err != nil || gid == 0 {
		log.Fatal("Telegram Forum Group ID dorost nist")
	}

	b := telegram.New(c.TelegramToken, 0)
	fmt.Println("🔎 Dar hale check-e Forum va permission haye bot...")
	if err := b.ValidateForum(gid); err != nil {
		log.Fatalf("forum validation failed: %v", err)
	}

	c.TelegramGroupID = gid
	if err := config.Save("", c); err != nil {
		log.Fatal(err)
	}
	fmt.Println("✅ Forum group validated and saved")

	if err := exec.Command("systemctl", "restart", "marzwatch").Run(); err != nil {
		log.Fatalf("config saved but marzwatch restart failed: %v", err)
	}
	fmt.Println("✅ MarzWatch restarted")
	fmt.Println("🛰 Overview va Node topic-ha khodkar sync mishan.")
}
