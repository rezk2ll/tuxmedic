package checks

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

type aptSource struct{ file, uri, suite string }

const aptLists = "/var/lib/apt/lists/"

// aptSourcesIn reads both the one-line .list format and deb822 .sources.
func aptSourcesIn(s sys.System) []aptSource {
	var out []aptSource
	lists, _ := s.Glob("/etc/apt/sources.list.d/*.list")
	for _, f := range append([]string{"/etc/apt/sources.list"}, lists...) {
		for _, l := range strings.Split(readTrim(s, f), "\n") {
			l, _, _ = strings.Cut(l, "#")
			fs := strings.Fields(l)
			if len(fs) < 3 || fs[0] != "deb" {
				continue
			}
			fs = fs[1:]
			if strings.HasPrefix(fs[0], "[") {
				for len(fs) > 0 && !strings.HasSuffix(fs[0], "]") {
					fs = fs[1:]
				}
				if len(fs) > 0 {
					fs = fs[1:]
				}
			}
			if len(fs) >= 2 {
				out = append(out, aptSource{f, fs[0], fs[1]})
			}
		}
	}
	deb822, _ := s.Glob("/etc/apt/sources.list.d/*.sources")
	for _, f := range deb822 {
		for _, stanza := range strings.Split(readTrim(s, f), "\n\n") {
			kv := map[string]string{}
			for _, l := range strings.Split(stanza, "\n") {
				if k, v, ok := strings.Cut(l, ":"); ok && !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "#") {
					kv[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
				}
			}
			if strings.EqualFold(kv["enabled"], "no") || !strings.Contains(" "+kv["types"]+" ", " deb ") {
				continue
			}
			for _, u := range strings.Fields(kv["uris"]) {
				for _, su := range strings.Fields(kv["suites"]) {
					out = append(out, aptSource{f, u, su})
				}
			}
		}
	}
	return out
}

// aptListName is apt's URItoFileName for a suite's Release file prefix.
func aptListName(uri, suite string) (string, bool) {
	scheme, rest, ok := strings.Cut(uri, "://")
	if !ok || (scheme != "http" && scheme != "https") || strings.HasSuffix(suite, "/") {
		return "", false
	}
	host, path, _ := strings.Cut(rest, "/")
	if i := strings.LastIndex(host, "@"); i >= 0 {
		host = host[i+1:]
	}
	return strings.ReplaceAll(aptQuote(strings.TrimSuffix(host+"/"+path, "/")+"/dists/"+suite+"/"), "/", "_"), true
}

// aptQuote is apt's QuoteString with the set URItoFileName passes, so
// "node_22.x" is stored as "node%5f22.x".
func aptQuote(u string) string {
	var b strings.Builder
	for i := 0; i < len(u); i++ {
		c := u[i]
		if c <= 0x20 || c >= 0x7f || strings.IndexByte(`\|{}[]<>"^~_=!@#$%&*`, c) >= 0 {
			fmt.Fprintf(&b, "%%%02x", c)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

var ppaURI = regexp.MustCompile(`^https?://ppa\.launchpad(?:content)?\.net/([^/]+)/([^/]+)/`)

// deadAptSources finds sources apt has never managed to download, the usual
// cause of "apt update" failing: a PPA that dropped this release, or a repo
// whose signing key expired.
func deadAptSources(s sys.System) []doctor.Finding {
	have, _ := s.Glob(aptLists + "*Release")
	if len(have) == 0 {
		return nil
	}
	got := map[string]bool{}
	for _, h := range have {
		got[filepath.Base(h)] = true
	}
	var bad []aptSource
	seen := map[string]bool{}
	for _, src := range aptSourcesIn(s) {
		name, ok := aptListName(src.uri, src.suite)
		if !ok || got[name+"InRelease"] || got[name+"Release"] || seen[name] {
			continue
		}
		seen[name] = true
		bad = append(bad, src)
	}
	if len(bad) == 0 {
		return nil
	}
	var ev []string
	var fixes []doctor.Fix
	for _, b := range bad {
		ev = append(ev, b.file+": "+b.uri+" "+b.suite)
		if m := ppaURI.FindStringSubmatch(b.uri); m != nil {
			fixes = append(fixes, doctor.Fix{Title: "Remove the ppa:" + m[1] + "/" + m[2] + " source", Command: "sudo add-apt-repository --remove ppa:" + m[1] + "/" + m[2], NeedsRoot: true})
		}
	}
	fixes = append(fixes,
		doctor.Fix{Title: "See the error for each one", Command: "sudo apt update", NeedsRoot: true},
		doctor.Fix{Title: "Turn off the ones you don't need in Software & Updates, under Other Software"},
	)
	title := "apt can't download from " + plural(len(bad), "source", "sources")
	if len(bad) == 1 {
		title = "apt can't download from " + bad[0].uri
	}
	return []doctor.Finding{{
		Status: doctor.Warn,
		Title:  title,
		Explain: "apt has no package list from it, so \"apt update\" ends with an error and the software center may refuse to update. " +
			"Usually the source dropped your release, moved, or its signing key expired. If you added it just now, run sudo apt update first.",
		Fixes:    fixes,
		Evidence: ev,
	}}
}
