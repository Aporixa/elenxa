// Package evidenceimport parses scanner output into normalized observations.
// Parsed records are evidence, not confirmed vulnerability findings.
package evidenceimport

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

const MaxInputBytes = 32 << 20

type Provenance struct {
	Format  string `json:"format"`
	Source  string `json:"source"`
	Digest  string `json:"digest"`
	Tool    string `json:"tool,omitempty"`
	Version string `json:"version,omitempty"`
}

type Observation struct {
	ID         string     `json:"id,omitempty"`
	Title      string     `json:"title"`
	Severity   string     `json:"severity,omitempty"`
	Target     string     `json:"target,omitempty"`
	Evidence   string     `json:"evidence"`
	Confidence string     `json:"confidence"`
	Provenance Provenance `json:"provenance"`
}

type Result struct {
	Provenance   Provenance    `json:"provenance"`
	Observations []Observation `json:"observations"`
}

func Parse(format, source string, input io.Reader) (Result, error) {
	format = strings.ToLower(strings.TrimSpace(format))
	limited := io.LimitReader(input, MaxInputBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return Result{}, err
	}
	if len(raw) > MaxInputBytes {
		return Result{}, fmt.Errorf("scanner result exceeds %d bytes", MaxInputBytes)
	}
	hash := sha256.Sum256(raw)
	provenance := Provenance{Format: format, Source: source, Digest: "sha256:" + hex.EncodeToString(hash[:])}
	var observations []Observation
	switch format {
	case "sarif":
		observations, provenance.Tool, provenance.Version, err = parseSARIF(raw)
	case "nmap-xml", "nmap":
		provenance.Format = "nmap-xml"
		observations, provenance.Tool, provenance.Version, err = parseNmap(raw)
	case "nuclei-jsonl", "nuclei":
		provenance.Format = "nuclei-jsonl"
		observations, provenance.Tool, provenance.Version, err = parseNuclei(raw)
	default:
		return Result{}, fmt.Errorf("unsupported scanner format %q (use sarif, nmap-xml, or nuclei-jsonl)", format)
	}
	if err != nil {
		return Result{}, err
	}
	for i := range observations {
		observations[i].Provenance = provenance
	}
	return Result{Provenance: provenance, Observations: observations}, nil
}

func parseSARIF(raw []byte) ([]Observation, string, string, error) {
	var doc struct {
		Version string `json:"version"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					Name    string `json:"name"`
					Version string `json:"version"`
				} `json:"driver"`
			} `json:"tool"`
			Results []struct {
				RuleID  string `json:"ruleId"`
				Level   string `json:"level"`
				Message struct {
					Text string `json:"text"`
				} `json:"message"`
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct {
							URI string `json:"uri"`
						} `json:"artifactLocation"`
						Region struct {
							StartLine int `json:"startLine"`
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, "", "", fmt.Errorf("decode SARIF: %w", err)
	}
	var out []Observation
	toolName, toolVersion := "sarif", doc.Version
	for _, run := range doc.Runs {
		if run.Tool.Driver.Name != "" {
			toolName = run.Tool.Driver.Name
		}
		if run.Tool.Driver.Version != "" {
			toolVersion = run.Tool.Driver.Version
		}
		for _, result := range run.Results {
			target := ""
			if len(result.Locations) > 0 {
				target = result.Locations[0].PhysicalLocation.ArtifactLocation.URI
				if line := result.Locations[0].PhysicalLocation.Region.StartLine; line > 0 {
					target += fmt.Sprintf(":%d", line)
				}
			}
			title := strings.TrimSpace(result.Message.Text)
			if title == "" {
				title = "SARIF result " + result.RuleID
			}
			out = append(out, Observation{ID: result.RuleID, Title: title, Severity: normalizeSeverity(result.Level), Target: target, Evidence: title, Confidence: "observed"})
		}
	}
	return out, toolName, toolVersion, nil
}

func parseNmap(raw []byte) ([]Observation, string, string, error) {
	var doc struct {
		Scanner string `xml:"scanner,attr"`
		Version string `xml:"version,attr"`
		Hosts   []struct {
			Addresses []struct {
				Addr string `xml:"addr,attr"`
				Type string `xml:"addrtype,attr"`
			} `xml:"address"`
			Hostnames []struct {
				Name string `xml:"name,attr"`
			} `xml:"hostnames>hostname"`
			Ports []struct {
				Protocol string `xml:"protocol,attr"`
				PortID   int    `xml:"portid,attr"`
				State    struct {
					State  string `xml:"state,attr"`
					Reason string `xml:"reason,attr"`
				} `xml:"state"`
				Service struct {
					Name    string `xml:"name,attr"`
					Product string `xml:"product,attr"`
					Version string `xml:"version,attr"`
				} `xml:"service"`
			} `xml:"ports>port"`
		} `xml:"host"`
	}
	if err := xml.Unmarshal(raw, &doc); err != nil {
		return nil, "", "", fmt.Errorf("decode Nmap XML: %w", err)
	}
	var out []Observation
	for _, host := range doc.Hosts {
		target := ""
		if len(host.Addresses) > 0 {
			target = host.Addresses[0].Addr
		}
		if len(host.Hostnames) > 0 && host.Hostnames[0].Name != "" {
			target = host.Hostnames[0].Name + " (" + target + ")"
		}
		for _, port := range host.Ports {
			service := strings.TrimSpace(strings.Join([]string{port.Service.Name, port.Service.Product, port.Service.Version}, " "))
			evidence := fmt.Sprintf("%s/%d state=%s reason=%s service=%s", port.Protocol, port.PortID, port.State.State, port.State.Reason, service)
			out = append(out, Observation{ID: fmt.Sprintf("%s/%d", port.Protocol, port.PortID), Title: fmt.Sprintf("Port %s/%d is %s", port.Protocol, port.PortID, port.State.State), Severity: "info", Target: target, Evidence: evidence, Confidence: "observed"})
		}
	}
	tool := doc.Scanner
	if tool == "" {
		tool = "nmap"
	}
	return out, tool, doc.Version, nil
}

func parseNuclei(raw []byte) ([]Observation, string, string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	var out []Observation
	line := 0
	for scanner.Scan() {
		line++
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var row struct {
			TemplateID  string `json:"template-id"`
			MatchedAt   string `json:"matched-at"`
			Host        string `json:"host"`
			MatcherName string `json:"matcher-name"`
			Type        string `json:"type"`
			Info        struct {
				Name     string `json:"name"`
				Severity string `json:"severity"`
			} `json:"info"`
			Extracted []string `json:"extracted-results"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return nil, "", "", fmt.Errorf("decode Nuclei JSONL line %d: %w", line, err)
		}
		target := row.MatchedAt
		if target == "" {
			target = row.Host
		}
		evidence := strings.TrimSpace(strings.Join([]string{row.MatcherName, row.Type, strings.Join(row.Extracted, ", ")}, " "))
		if evidence == "" {
			evidence = "Nuclei template matched " + target
		}
		title := row.Info.Name
		if title == "" {
			title = row.TemplateID
		}
		out = append(out, Observation{ID: row.TemplateID, Title: title, Severity: normalizeSeverity(row.Info.Severity), Target: target, Evidence: evidence, Confidence: "observed"})
	}
	if err := scanner.Err(); err != nil {
		return nil, "", "", err
	}
	return out, "nuclei", "", nil
}

func normalizeSeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "error", "critical":
		return "critical"
	case "warning", "high":
		return "high"
	case "medium":
		return "medium"
	case "note", "low":
		return "low"
	default:
		return "info"
	}
}
