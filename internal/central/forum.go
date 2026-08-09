package central

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/SevinEW/marzban-monitoring/internal/model"
)

const forumStatePath = "/var/lib/marzwatch/forum.json"
const staleNodeTTL = 6 * time.Hour

type forumNodeState struct {
	TopicID   int    `json:"topic_id"`
	MessageID int    `json:"message_id"`
	TopicName string `json:"topic_name"`
	LastText  string `json:"last_text,omitempty"`
}

type forumState struct {
	OverviewTopicID   int                       `json:"overview_topic_id"`
	OverviewMessageID int                       `json:"overview_message_id"`
	OverviewLastText  string                    `json:"overview_last_text,omitempty"`
	Nodes             map[string]forumNodeState `json:"nodes"`
}

func loadForumState() forumState {
	st := forumState{Nodes: map[string]forumNodeState{}}
	b, err := os.ReadFile(forumStatePath)
	if err != nil {
		return st
	}
	if err := json.Unmarshal(b, &st); err != nil {
		log.Printf("forum state decode error: %v", err)
		return forumState{Nodes: map[string]forumNodeState{}}
	}
	if st.Nodes == nil {
		st.Nodes = map[string]forumNodeState{}
	}
	return st
}

func saveForumState(st forumState) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := forumStatePath + ".tmp"
	if err := os.WriteFile(tmp, b, 0640); err != nil {
		return err
	}
	if err := os.Rename(tmp, forumStatePath); err != nil {
		return err
	}
	return os.Chmod(forumStatePath, 0640)
}

func (s *Server) forumLoop() {
	if s.cfg.TelegramGroupID == 0 {
		<-s.stop
		return
	}

	st := loadForumState()
	for {
		if err := s.bot.ValidateForum(s.cfg.TelegramGroupID); err != nil {
			log.Printf("telegram forum validation: %v", err)
			select {
			case <-time.After(time.Minute):
				continue
			case <-s.stop:
				return
			}
		}
		break
	}

	if err := s.syncForum(&st); err != nil {
		log.Printf("telegram forum initial sync: %v", err)
	}

	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			if err := s.syncForum(&st); err != nil {
				log.Printf("telegram forum sync: %v", err)
			}
		case <-s.stop:
			return
		}
	}
}

func (s *Server) syncForum(st *forumState) error {
	now := time.Now()
	changed := false

	// Remove agents that have been silent for more than six hours. Central itself
	// is intentionally exempt from automatic deletion.
	for _, n := range s.store.Nodes() {
		if n.ID == "central" || n.LastSeen.IsZero() || now.Sub(n.LastSeen) <= staleNodeTTL {
			continue
		}
		if fs, ok := st.Nodes[n.ID]; ok {
			if fs.TopicID != 0 {
				if err := s.bot.DeleteForumTopic(s.cfg.TelegramGroupID, fs.TopicID); err != nil {
					log.Printf("delete stale node topic %s: %v", n.Name, err)
				}
			}
			delete(st.Nodes, n.ID)
			changed = true
		}
		if _, ok := s.store.RemoveNode(n.ID); ok {
			delete(s.alerts, n.ID)
			_ = s.store.Flush()
			log.Printf("stale node auto-removed after 6h silence: %s (%s)", n.Name, n.ID)
		}
	}

	nodes := s.store.Nodes()
	if st.OverviewTopicID == 0 {
		topicID, err := s.bot.CreateForumTopic(s.cfg.TelegramGroupID, "💠 Overview")
		if err != nil {
			return fmt.Errorf("create overview topic: %w", err)
		}
		st.OverviewTopicID = topicID
		changed = true
	}
	overview := formatOverviewLive(nodes, now.In(s.tz))
	if st.OverviewMessageID == 0 {
		msgID, err := s.bot.SendMessage(s.cfg.TelegramGroupID, st.OverviewTopicID, overview)
		if err != nil {
			return fmt.Errorf("create overview live message: %w", err)
		}
		st.OverviewMessageID = msgID
		st.OverviewLastText = overview
		changed = true
	} else if overview != st.OverviewLastText {
		if err := s.bot.EditMessage(s.cfg.TelegramGroupID, st.OverviewMessageID, overview); err != nil {
			msgID, sendErr := s.bot.SendMessage(s.cfg.TelegramGroupID, st.OverviewTopicID, overview)
			if sendErr != nil {
				return fmt.Errorf("edit overview: %v; recreate: %w", err, sendErr)
			}
			st.OverviewMessageID = msgID
		}
		st.OverviewLastText = overview
		changed = true
	}

	date := now.In(s.tz).Format("2006-01-02")
	seen := map[string]bool{}
	for _, n := range nodes {
		seen[n.ID] = true
		name := forumTopicName(n)
		fs := st.Nodes[n.ID]
		if fs.TopicID == 0 {
			topicID, err := s.bot.CreateForumTopic(s.cfg.TelegramGroupID, name)
			if err != nil {
				log.Printf("create topic for %s: %v", n.Name, err)
				continue
			}
			fs.TopicID = topicID
			fs.TopicName = name
			changed = true
		} else if fs.TopicName != name {
			if err := s.bot.EditForumTopic(s.cfg.TelegramGroupID, fs.TopicID, name); err != nil {
				log.Printf("rename topic for %s: %v", n.Name, err)
			} else {
				fs.TopicName = name
				changed = true
			}
		}

		on := !n.LastSeen.IsZero() && now.Sub(n.LastSeen) < 150*time.Second
		text := formatNodeBlock(n, on, date)
		if fs.MessageID == 0 {
			msgID, err := s.bot.SendMessage(s.cfg.TelegramGroupID, fs.TopicID, text)
			if err != nil {
				log.Printf("create live card for %s: %v", n.Name, err)
				st.Nodes[n.ID] = fs
				continue
			}
			fs.MessageID = msgID
			fs.LastText = text
			changed = true
		} else if text != fs.LastText {
			if err := s.bot.EditMessage(s.cfg.TelegramGroupID, fs.MessageID, text); err != nil {
				msgID, sendErr := s.bot.SendMessage(s.cfg.TelegramGroupID, fs.TopicID, text)
				if sendErr != nil {
					log.Printf("edit live card for %s: %v; recreate: %v", n.Name, err, sendErr)
					st.Nodes[n.ID] = fs
					continue
				}
				fs.MessageID = msgID
			}
			fs.LastText = text
			changed = true
		}
		st.Nodes[n.ID] = fs
	}

	// Clean orphan forum mappings if the corresponding node disappeared by any
	// other administrative path.
	for id, fs := range st.Nodes {
		if seen[id] {
			continue
		}
		if fs.TopicID != 0 {
			if err := s.bot.DeleteForumTopic(s.cfg.TelegramGroupID, fs.TopicID); err != nil {
				log.Printf("delete orphan topic %s: %v", fs.TopicName, err)
			}
		}
		delete(st.Nodes, id)
		changed = true
	}

	if changed {
		return saveForumState(*st)
	}
	return nil
}

func forumTopicName(n model.Node) string {
	name := strings.TrimSpace(n.Name)
	if name == "" {
		name = "Node"
	}
	return flagEmoji(n.Location.CountryCode) + " " + name
}

func formatOverviewLive(nodes []model.Node, now time.Time) string {
	var stable, warning, critical, offline int
	var rx, tx float64
	var todayRX, todayTX uint64
	var healthTotal int
	var online int
	date := now.Format("2006-01-02")

	type row struct {
		name   string
		status string
		health int
	}
	rows := make([]row, 0, len(nodes))

	for _, n := range nodes {
		on := !n.LastSeen.IsZero() && now.Sub(n.LastSeen) < 150*time.Second
		cpu := n.Latest.CPUPercent
		ram := pct(n.Latest.MemUsed, n.Latest.MemTotal)
		disk := pct(n.Latest.DiskUsed, n.Latest.DiskTotal)
		worst := max3(cpu, ram, disk)
		status := "🟢"
		if !on {
			offline++
			status = "🔴"
		} else {
			online++
			rx += n.Latest.RXBps
			tx += n.Latest.TXBps
			h := health(n, true)
			healthTotal += h
			switch {
			case worst >= 95:
				critical++
				status = "🔴"
			case worst >= 85:
				warning++
				status = "🟠"
			default:
				stable++
			}
		}
		if ds, ok := n.Daily[date]; ok {
			todayRX += ds.RXBytes
			todayTX += ds.TXBytes
		}
		rows = append(rows, row{name: forumTopicName(n), status: status, health: health(n, on)})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	globalHealth := 0
	if online > 0 {
		globalHealth = healthTotal / online
	}

	var list strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&list, "%s %s  •  🛡 %d/100\n", r.status, r.name, r.health)
	}

	return fmt.Sprintf(`╭━━━━━━━━━━━━━━━━━━━━━━━━━━╮
┃ 💠 MARZWATCH • LIVE OVERVIEW
┣━━━━━━━━━━━━━━━━━━━━━━━━━━┫
┃ 🕒 %s
┃
┃ 🛰 NODES      %d
┃ 🟢 STABLE     %d
┃ 🟠 WARNING    %d
┃ 🔴 CRITICAL   %d
┃ 🔴 OFFLINE    %d
┃
┃ 📡 LIVE NETWORK
┃ ⬇️ %s   ⬆️ %s
┃ ↕️ %s
┃
┃ 📦 TODAY      %s
┃ 🛡 HEALTH     %d/100
┣━━━━━━━━━━━━━━━━━━━━━━━━━━┫
%s╰━━━━━━━━━━━━━━━━━━━━━━━━━━╯`,
		now.Format("15:04:05"), len(nodes), stable, warning, critical, offline,
		rate(rx), rate(tx), rate(rx+tx), bytes(todayRX+todayTX), globalHealth, list.String())
}

func max3(a, b, c float64) float64 {
	if b > a {
		a = b
	}
	if c > a {
		a = c
	}
	return a
}
