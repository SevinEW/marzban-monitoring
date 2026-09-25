package geo

import (
	"encoding/json"
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"io"
	"net"
	"strings"
)

func PublicIP(s string) string {
	ip := net.ParseIP(strings.TrimSpace(s))
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return ""
	}
	return ip.String()
}

func Detect() model.Location {
	candidates := map[string]bool{}
	if interfaces, err := net.Interfaces(); err == nil {
		for _, iface := range interfaces {
			name := strings.ToLower(iface.Name)
			if iface.Flags&net.FlagUp == 0 || strings.HasPrefix(name, "wg") || strings.HasPrefix(name, "tun") || strings.HasPrefix(name, "tailscale") {
				continue
			}
			addrs, _ := iface.Addrs()
			for _, addr := range addrs {
				ip, _, err := net.ParseCIDR(addr.String())
				if err == nil && ip.To4() != nil && PublicIP(ip.String()) != "" {
					candidates[ip.String()] = true
				}
			}
		}
	}
	if len(candidates) == 1 {
		for ip := range candidates {
			return Lookup(ip)
		}
	}
	for _, url := range []string{"https://api.ipify.org", "https://checkip.amazonaws.com"} {
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 128))
		resp.Body.Close()
		if err == nil && resp.StatusCode == 200 {
			if ip := PublicIP(string(data)); ip != "" {
				return Lookup(ip)
			}
		}
	}
	return model.Location{}
}

func Lookup(ip string) model.Location { loc, _ := Resolve(ip); return loc }

func Resolve(ip string) (model.Location, bool) {
	ip = PublicIP(ip)
	if ip == "" {
		return model.Location{}, false
	}
	var a, b model.Location
	var who struct {
		IP      string `json:"ip"`
		Success bool   `json:"success"`
		Country string `json:"country"`
		Code    string `json:"country_code"`
		City    string `json:"city"`
		Region  string `json:"region"`
	}
	if getJSON("https://ipwho.is/"+ip, &who) && who.Success && PublicIP(who.IP) == ip {
		a = model.Location{PublicIP: ip, Country: who.Country, CountryCode: strings.ToUpper(who.Code), City: who.City, Region: who.Region}
	}
	var api struct {
		IP      string `json:"ip"`
		Error   bool   `json:"error"`
		Country string `json:"country_name"`
		Code    string `json:"country_code"`
		City    string `json:"city"`
		Region  string `json:"region"`
	}
	if getJSON("https://ipapi.co/"+ip+"/json/", &api) && !api.Error && PublicIP(api.IP) == ip {
		b = model.Location{PublicIP: ip, Country: api.Country, CountryCode: strings.ToUpper(api.Code), City: api.City, Region: api.Region}
	}
	return consensus(ip, a, b), a.CountryCode != "" || b.CountryCode != ""
}

func getJSON(url string, out any) bool {
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200 && json.NewDecoder(io.LimitReader(resp.Body, 16384)).Decode(out) == nil
}

func consensus(ip string, a, b model.Location) model.Location {
	valid := func(s string) bool { return len(s) == 2 && s[0] >= 'A' && s[0] <= 'Z' && s[1] >= 'A' && s[1] <= 'Z' }
	if !valid(a.CountryCode) {
		a = model.Location{}
	}
	if !valid(b.CountryCode) {
		b = model.Location{}
	}
	if a.CountryCode == "" {
		b.PublicIP = ip
		return b
	}
	if b.CountryCode == "" {
		a.PublicIP = ip
		return a
	}
	if a.CountryCode != b.CountryCode {
		return model.Location{PublicIP: ip}
	}
	if !strings.EqualFold(a.City, b.City) {
		a.City = ""
	}
	if !strings.EqualFold(a.Region, b.Region) {
		a.Region = ""
	}
	a.PublicIP = ip
	return a
}
