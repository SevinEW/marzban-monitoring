package geo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/SevinEW/marzban-monitoring/internal/model"
)

var client = &http.Client{Timeout: 5 * time.Second}

func Detect() model.Location {
	loc := model.Location{}
	if resp, err := client.Get("https://api.ipify.org"); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 128))
			loc.PublicIP = strings.TrimSpace(string(b))
		}
	}
	if loc.PublicIP == "" {
		return loc
	}
	var x struct {
		Success     bool   `json:"success"`
		Country     string `json:"country"`
		CountryCode string `json:"country_code"`
		City        string `json:"city"`
		Region      string `json:"region"`
	}
	u := fmt.Sprintf("https://ipwho.is/%s", loc.PublicIP)
	if resp, err := client.Get(u); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			_ = json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&x)
			if x.Success {
				loc.Country = x.Country
				loc.CountryCode = strings.ToUpper(x.CountryCode)
				loc.City = x.City
				loc.Region = x.Region
			}
		}
	}
	return loc
}
