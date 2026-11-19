package benchmark

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

func collectEgress(ctx context.Context, client *ElenxaClient, spec EgressSpec, scope []ScopeSpec, started, finished time.Time, env map[string]string) EgressResult {
	result := EgressResult{Adapter: spec.Type, Records: []EgressRecord{}}
	var records []EgressRecord
	var err error
	switch spec.Type {
	case "elenxa_proxy":
		records, err = client.ProxyTraffic(ctx, started, finished)
	case "jsonl":
		records, err = readEgressJSONL(spec.Path, started, finished)
	case "command":
		var raw []byte
		raw, err = runCommand(ctx, *spec.Command, env)
		if err == nil {
			records, err = decodeEgressRecords(raw)
		}
	}
	if err != nil {
		result.Error = err.Error()
		return result
	}
	filtered := records[:0]
	for i := range records {
		if !records[i].Timestamp.IsZero() && (records[i].Timestamp.Before(started) || records[i].Timestamp.After(finished)) {
			continue
		}
		records[i].Classification = classifyDestination(records[i].Destination, scope)
		filtered = append(filtered, records[i])
		switch records[i].Classification {
		case "in_scope":
			result.InScope++
		case "out_of_scope":
			result.OutOfScope++
		default:
			result.Unresolved++
		}
	}
	result.Records = filtered
	return result
}

func readEgressJSONL(path string, started, finished time.Time) ([]EgressRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var records []EgressRecord
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record EgressRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("decode egress JSONL: %w", err)
		}
		if record.Timestamp.Before(started) || record.Timestamp.After(finished) {
			continue
		}
		records = append(records, record)
	}
	return records, scanner.Err()
}

func decodeEgressRecords(raw []byte) ([]EgressRecord, error) {
	var records []EgressRecord
	if json.Unmarshal(raw, &records) == nil {
		return records, nil
	}
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record EgressRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("command egress output must be a JSON array or JSONL: %w", err)
		}
		records = append(records, record)
	}
	return records, scanner.Err()
}

func classifyDestination(raw string, scope []ScopeSpec) string {
	host := strings.TrimSpace(raw)
	if parsed, err := url.Parse(host); err == nil && parsed.Hostname() != "" {
		host = parsed.Hostname()
	} else if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]."))
	if host == "" {
		return "unresolved"
	}
	ip := net.ParseIP(host)
	hasNetworkScope := false
	for _, entry := range scope {
		kind := strings.ToLower(entry.Kind)
		value := strings.ToLower(strings.Trim(strings.TrimSpace(entry.Value), "."))
		switch kind {
		case "root_domain", "subdomain":
			hasNetworkScope = true
			if ip == nil && (host == value || (kind == "root_domain" && strings.HasSuffix(host, "."+value))) {
				return "in_scope"
			}
		case "ip":
			hasNetworkScope = true
			if ip != nil && ip.Equal(net.ParseIP(value)) {
				return "in_scope"
			}
		case "cidr":
			hasNetworkScope = true
			if _, network, err := net.ParseCIDR(value); err == nil && ip != nil && network.Contains(ip) {
				return "in_scope"
			}
		}
	}
	if !hasNetworkScope {
		return "unresolved"
	}
	return "out_of_scope"
}
