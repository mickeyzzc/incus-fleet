package telemetry

import (
	"encoding/csv"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Prometheus renders series as Prometheus text exposition with timestamps.
func Prometheus(series []Series) string {
	var b strings.Builder
	typed := make(map[string]string)
	for _, item := range series {
		typ := item.Type
		if typ == "" {
			typ = "gauge"
		}
		if typed[item.Name] == "" {
			typed[item.Name] = typ
			fmt.Fprintf(&b, "# TYPE %s %s\n", item.Name, typ)
		}
		for _, point := range item.Points {
			b.WriteString(item.Name)
			writeLabels(&b, item.Labels)
			b.WriteByte(' ')
			b.WriteString(strconv.FormatFloat(point.Value, 'g', -1, 64))
			b.WriteByte(' ')
			b.WriteString(strconv.FormatInt(point.At.UnixMilli(), 10))
			b.WriteByte('\n')
		}
	}

	return b.String()
}

func writeLabels(b *strings.Builder, labels map[string]string) {
	if len(labels) == 0 {
		return
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	b.WriteByte('{')
	for i, key := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(key)
		b.WriteString(`="`)
		b.WriteString(strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(labels[key]))
		b.WriteByte('"')
	}
	b.WriteByte('}')
}

// CSV writes long-form rows: timestamp,metric,cluster,member,agent_id,value.
func CSV(series []Series) (string, error) {
	var b strings.Builder
	writer := csv.NewWriter(&b)
	if err := writer.Write([]string{"timestamp", "metric", "cluster", "member", "agent_id", "value"}); err != nil {
		return "", err
	}
	for _, item := range series {
		for _, point := range item.Points {
			if err := writer.Write([]string{
				point.At.UTC().Format("2006-01-02T15:04:05.000Z"),
				item.Name,
				item.Labels["cluster"],
				item.Labels["member"],
				item.Labels["agent_id"],
				strconv.FormatFloat(point.Value, 'g', -1, 64),
			}); err != nil {
				return "", err
			}
		}
	}
	writer.Flush()
	return b.String(), writer.Error()
}
