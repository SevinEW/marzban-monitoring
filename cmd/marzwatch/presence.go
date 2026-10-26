package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/user"
	"strconv"
	"time"

	"github.com/SevinEW/marzban-monitoring/internal/presence"
)

func bindPresence() {
	mustRoot()
	flags := flag.NewFlagSet("presence-bind", flag.ExitOnError)
	address := flags.String("address", "", "existing loopback Xray StatsService address")
	pid := flags.Int("pid", 0, "running Xray PID")
	namespace := flags.String("namespace", "", "shared random namespace for one panel's account IDs")
	attest := flags.Bool("attest-running-policy", false, "operator verified statsUserOnline for every active user level in THIS running core")
	flags.Parse(os.Args[2:])
	if !*attest {
		log.Fatal("Verify the running core policy first; this command does not enable or reload Xray")
	}
	start, e := presence.ProcessStart(*pid)
	if e != nil {
		log.Fatal("Running core could not be verified")
	}
	binding := presence.Binding{Schema: 1, Namespace: *namespace, TrackingVerified: true, Cores: []presence.Core{{Address: *address, PID: *pid, StartTicks: start}}}
	if presence.ValidateBinding(binding) != nil {
		log.Fatal("Invalid binding")
	}
	if presence.VerifySocketOwner(binding.Cores[0]) != nil {
		log.Fatal("The API socket does not belong to this Xray process")
	}
	after, e := presence.ProcessStart(*pid)
	if e != nil || after != start {
		log.Fatal("Core restarted during binding; retry after verifying its policy")
	}
	raw, e := json.MarshalIndent(binding, "", "  ")
	if e != nil {
		log.Fatal("Cannot encode binding")
	}
	group, e := user.LookupGroup("marzwatch")
	if e != nil {
		log.Fatal("marzwatch group missing")
	}
	gid, e := strconv.Atoi(group.Gid)
	if e != nil {
		log.Fatal("Invalid service group")
	}
	f, e := os.CreateTemp("/etc/marzwatch", ".presence-*")
	if e != nil {
		log.Fatal("Cannot stage binding")
	}
	path := f.Name()
	defer os.Remove(path)
	if _, e = f.Write(raw); e == nil {
		e = f.Sync()
	}
	if err := f.Close(); e == nil {
		e = err
	}
	if e == nil {
		e = os.Chown(path, 0, gid)
	}
	if e == nil {
		e = os.Chmod(path, 0640)
	}
	if e == nil {
		e = os.Rename(path, presence.ConfigPath)
	}
	if e != nil {
		log.Fatal("Cannot save binding")
	}
	fmt.Println("Presence binding saved. Monitoring reloads it automatically. Xray was not restarted or changed.")
}
func checkPresence() {
	flags := flag.NewFlagSet("presence-check", flag.ExitOnError)
	samples := flags.Int("samples", 1, "samples (1-60)")
	binding := flags.String("binding", presence.ConfigPath, "binding file to check")
	flags.Parse(os.Args[2:])
	if *samples < 1 || *samples > 60 {
		log.Fatal("samples must be 1-60")
	}
	c := presence.NewCollector(*binding)
	defer c.Close()
	for i := 0; i < *samples; i++ {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		sample := c.Sample(ctx)
		cancel()
		var count *int
		if sample.Status == "ok" {
			n := len(sample.Users)
			count = &n
		}
		b, _ := json.Marshal(struct {
			Status   string  `json:"status"`
			Users    *int    `json:"users"`
			Duration float64 `json:"duration_ms"`
		}{sample.Status, count, float64(time.Since(start).Microseconds()) / 1000})
		fmt.Println(string(b))
		if i+1 < *samples {
			time.Sleep(max(0, time.Second-time.Since(start)))
		}
	}
}
