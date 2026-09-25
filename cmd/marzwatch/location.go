package main

import (
	"fmt"
	"github.com/SevinEW/marzban-monitoring/internal/config"
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"github.com/SevinEW/marzban-monitoring/internal/storage"
	"log"
	"os"
	"strings"
)

func setNodeLocation() {
	if len(os.Args) < 4 {
		log.Fatal("Usage on Central: marzwatchctl location NODE_NAME COUNTRY_CODE [CITY]; use auto to remove override")
	}
	cfg, err := config.Load("")
	if err != nil {
		log.Fatal(err)
	}
	if cfg.Role != "central" {
		log.Fatal("Run this command on Central")
	}
	store, err := storage.Open("/var/lib/marzwatch/state.json")
	if err != nil {
		log.Fatal(err)
	}
	var ids []string
	for _, n := range store.Nodes() {
		if n.ID == os.Args[2] || strings.EqualFold(n.Name, os.Args[2]) {
			ids = append(ids, n.ID)
		}
	}
	if len(ids) != 1 {
		log.Fatal("Node name must match exactly one saved node; use its node ID if names are duplicated")
	}
	cc := strings.ToUpper(strings.TrimSpace(os.Args[3]))
	if cfg.LocationOverrides == nil {
		cfg.LocationOverrides = map[string]model.Location{}
	}
	if cc == "AUTO" {
		delete(cfg.LocationOverrides, ids[0])
	} else {
		if len(cc) != 2 || cc[0] < 'A' || cc[0] > 'Z' || cc[1] < 'A' || cc[1] > 'Z' {
			log.Fatal("Country code must contain two uppercase letters, e.g. TR, PL, DE")
		}
		country := map[string]string{"TR": "Turkey", "PL": "Poland", "GB": "United Kingdom", "NL": "Netherlands", "DE": "Germany", "FI": "Finland", "FR": "France", "IT": "Italy", "US": "United States", "IR": "Iran", "SE": "Sweden", "CH": "Switzerland", "CA": "Canada"}[cc]
		if country == "" {
			country = cc
		}
		cfg.LocationOverrides[ids[0]] = model.Location{CountryCode: cc, Country: country, City: strings.Join(os.Args[4:], " ")}
	}
	if err := config.Save("", cfg); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Location preference saved. Restart marzwatch on Central to apply; node identity and history are preserved.")
}
