package central

import (
	"fmt"
	"github.com/SevinEW/marzban-monitoring/internal/model"
	"math"
	"sort"
	"strings"
)

func sortedForumNodes(nodes []model.Node) []model.Node {
	result := append([]model.Node(nil), nodes...)
	sort.Slice(result, func(i, j int) bool {
		if a, b := countryKey(result[i]), countryKey(result[j]); a != b {
			return a < b
		}
		a, b := strings.ToLower(strings.TrimSpace(result[i].Name)), strings.ToLower(strings.TrimSpace(result[j].Name))
		if a == b {
			return result[i].ID < result[j].ID
		}
		return naturalLess(a, b)
	})
	return result
}

func naturalLess(a, b string) bool {
	for len(a) > 0 && len(b) > 0 {
		if a[0] >= '0' && a[0] <= '9' && b[0] >= '0' && b[0] <= '9' {
			i, j := 0, 0
			for i < len(a) && a[i] >= '0' && a[i] <= '9' {
				i++
			}
			for j < len(b) && b[j] >= '0' && b[j] <= '9' {
				j++
			}
			x, y := strings.TrimLeft(a[:i], "0"), strings.TrimLeft(b[:j], "0")
			if len(x) != len(y) {
				return len(x) < len(y)
			}
			if x != y {
				return x < y
			}
			if i != j {
				return i < j
			}
			a, b = a[i:], b[j:]
			continue
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func cpuUsageDetail(n model.Node) string {
	if n.Cores <= 0 {
		return "vCPU capacity unavailable"
	}
	usage := math.Max(0, math.Min(100, n.Latest.CPUPercent)) / 100 * float64(n.Cores)
	return fmt.Sprintf("Used ≈ %.2f / %d vCPU", usage, n.Cores)
}
