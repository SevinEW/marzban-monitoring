package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/SevinEW/marzban-monitoring/internal/config"
	"github.com/SevinEW/marzban-monitoring/internal/storage"
)

func mergeNodes() {
	if len(os.Args) < 4 {
		log.Fatal("Usage on Central: marzwatchctl merge-nodes KEEP_NODE_ID OLD_NODE_ID [OLD_NODE_ID...]")
	}
	cfg, err := config.Load("")
	if err != nil {
		log.Fatal(err)
	}
	if cfg.Role != "central" {
		log.Fatal("Run this command on Central")
	}
	st, err := storage.Open("/var/lib/marzwatch/state.json")
	if err != nil {
		log.Fatal(err)
	}
	keep, ok := st.GetNode(os.Args[2])
	if !ok {
		log.Fatal("Keep node was not found")
	}
	if cfg.NodeAliases == nil {
		cfg.NodeAliases = map[string]string{}
	}
	for _, id := range os.Args[3:] {
		old, ok := st.GetNode(id)
		if !ok {
			log.Fatal("Old node was not found")
		}
		if old.Location.PublicIP == "" || old.Location.PublicIP != keep.Location.PublicIP || old.Hostname != keep.Hostname {
			log.Fatal("Nodes do not share both a saved IP and hostname; refusing this merge")
		}
		cfg.NodeAliases[id] = keep.ID
	}
	if err := st.ApplyAliases(cfg.NodeAliases); err != nil {
		log.Fatal(err)
	}
	// Only configuration is changed while the service is running. Its state
	// writer remains the sole owner of live statistics and archived records.
	backup := config.DefaultPath + ".before-merge-" + time.Now().UTC().Format("20060102T150405.000000000")
	b, err := os.ReadFile(config.DefaultPath)
	if err != nil {
		log.Fatal(err)
	}
	f, err := os.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		log.Fatal(err)
	}
	if _, err = f.Write(b); err != nil {
		_ = f.Close()
		log.Fatal(err)
	}
	if err = f.Close(); err != nil {
		log.Fatal(err)
	}
	if err = config.Save("", cfg); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Merge saved: %d archived identities -> %s. Restart marzwatch on Central to apply. History is retained.\n", len(os.Args)-3, keep.Name)
}
