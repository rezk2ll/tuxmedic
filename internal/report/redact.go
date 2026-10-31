// Package report renders results as plain text, JSON, or a Markdown report
// that is safe to paste into a public forum.
package report

import (
	"regexp"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
)

var (
	macRE   = regexp.MustCompile(`(?i)\b([0-9a-f]{2}[:_-]){5}[0-9a-f]{2}\b`)
	ipv4RE  = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	ipv6RE  = regexp.MustCompile(`(?i)\b(?:[0-9a-f]{1,4}:){3,7}[0-9a-f]{1,4}\b`)
	emailRE = regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.-]+`)
	uuidRE  = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-?[0-9a-f]{4}-?[0-9a-f]{4}-?[0-9a-f]{4}-?[0-9a-f]{12}\b`)
	// Serial-number-like tokens in device names, e.g. USB headsets: "..._202105190004-00".
	serialRE = regexp.MustCompile(`_[0-9A-Za-z]{10,}(-\d\d)?\b`)
)

var keepIPs = map[string]bool{"127.0.0.1": true, "127.0.0.53": true, "0.0.0.0": true, "1.1.1.1": true, "9.9.9.9": true}

// Redactor removes what identifies a person or a machine: names, addresses,
// serial numbers. It errs on the side of removing too much.
type Redactor struct {
	replacer *strings.Replacer
}

func NewRedactor(h *doctor.Host) *Redactor {
	var pairs []string
	if h.Home != "" && h.Home != "/" {
		pairs = append(pairs, h.Home, "~")
	}
	if len(h.Hostname) > 2 {
		pairs = append(pairs, h.Hostname, "<hostname>")
	}
	if len(h.User) > 2 && h.User != "root" {
		pairs = append(pairs, h.User, "<user>")
	}
	return &Redactor{replacer: strings.NewReplacer(pairs...)}
}

func (r *Redactor) String(s string) string {
	s = r.replacer.Replace(s)
	s = emailRE.ReplaceAllString(s, "<email>")
	s = macRE.ReplaceAllString(s, "<mac>")
	s = uuidRE.ReplaceAllString(s, "<id>")
	s = ipv6RE.ReplaceAllString(s, "<ipv6>")
	s = ipv4RE.ReplaceAllStringFunc(s, func(ip string) string {
		if keepIPs[ip] || strings.HasPrefix(ip, "127.") {
			return ip
		}
		return "<ip>"
	})
	return serialRE.ReplaceAllString(s, "_<serial>")
}

func (r *Redactor) Report(rep doctor.Report) doctor.Report {
	out := rep
	out.Findings = make([]doctor.Finding, len(rep.Findings))
	for i, f := range rep.Findings {
		g := f
		g.Title, g.Explain = r.String(f.Title), r.String(f.Explain)
		g.Evidence = make([]string, len(f.Evidence))
		for j, e := range f.Evidence {
			g.Evidence[j] = r.String(e)
		}
		g.Fixes = make([]doctor.Fix, len(f.Fixes))
		for j, x := range f.Fixes {
			x.Title, x.Command = r.String(x.Title), r.String(x.Command)
			g.Fixes[j] = x
		}
		out.Findings[i] = g
	}
	return out
}
