package model

import "time"

type Location struct {
	PublicIP    string `json:"public_ip"`
	Country     string `json:"country"`
	CountryCode string `json:"country_code"`
	City        string `json:"city"`
	Region      string `json:"region"`
}

type Metric struct {
	Timestamp     time.Time `json:"timestamp"`
	CPUPercent    float64   `json:"cpu_percent"`
	Load1         float64   `json:"load1"`
	Load5         float64   `json:"load5"`
	Load15        float64   `json:"load15"`
	MemUsed       uint64    `json:"mem_used"`
	MemTotal      uint64    `json:"mem_total"`
	SwapUsed      uint64    `json:"swap_used"`
	SwapTotal     uint64    `json:"swap_total"`
	DiskUsed      uint64    `json:"disk_used"`
	DiskTotal     uint64    `json:"disk_total"`
	RXBytesTotal  uint64    `json:"rx_bytes_total"`
	TXBytesTotal  uint64    `json:"tx_bytes_total"`
	RXBps         float64   `json:"rx_bps"`
	TXBps         float64   `json:"tx_bps"`
	UptimeSeconds uint64    `json:"uptime_seconds"`
}

type RegisterRequest struct {
	Name     string   `json:"name"`
	Hostname string   `json:"hostname"`
	OS       string   `json:"os"`
	Arch     string   `json:"arch"`
	Cores    int      `json:"cores"`
	Location Location `json:"location"`
}

type RegisterResponse struct {
	NodeID     string `json:"node_id"`
	NodeSecret string `json:"node_secret"`
}

type DailyStats struct {
	Date         string    `json:"date"`
	Samples      int64     `json:"samples"`
	CPUSum       float64   `json:"cpu_sum"`
	CPUMin       float64   `json:"cpu_min"`
	CPUMax       float64   `json:"cpu_max"`
	RAMSum       float64   `json:"ram_sum"`
	RAMMin       float64   `json:"ram_min"`
	RAMMax       float64   `json:"ram_max"`
	DiskSum      float64   `json:"disk_sum"`
	DiskMin      float64   `json:"disk_min"`
	DiskMax      float64   `json:"disk_max"`
	RXBytes      uint64    `json:"rx_bytes"`
	TXBytes      uint64    `json:"tx_bytes"`
	PeakRXBps    float64   `json:"peak_rx_bps"`
	PeakTXBps    float64   `json:"peak_tx_bps"`
	PeakTotalBps float64   `json:"peak_total_bps"`
	PeakAt       time.Time `json:"peak_at"`
}

func NewDaily(date string) DailyStats {
	return DailyStats{Date: date, CPUMin: 101, RAMMin: 101, DiskMin: 101}
}

type Node struct {
	ID         string                `json:"id"`
	Secret     string                `json:"secret"`
	Name       string                `json:"name"`
	Hostname   string                `json:"hostname"`
	OS         string                `json:"os"`
	Arch       string                `json:"arch"`
	Cores      int                   `json:"cores"`
	Location   Location              `json:"location"`
	Registered time.Time             `json:"registered"`
	LastSeen   time.Time             `json:"last_seen"`
	Latest     Metric                `json:"latest"`
	Daily      map[string]DailyStats `json:"daily"`
}
