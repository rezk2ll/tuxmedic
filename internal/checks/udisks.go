package checks

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

const udisks = "org.freedesktop.UDisks2"

// udisksHealth reads drive health through UDisks2, which runs as root and
// shares SMART data over D-Bus, so no sudo is needed.
func udisksHealth(ctx context.Context, s sys.System) ([]doctor.Finding, bool) {
	if !s.Has("busctl") {
		return nil, false
	}
	out, err := s.Run(ctx, "busctl", "--list", "tree", udisks)
	if err != nil {
		return nil, false
	}
	var fs []doctor.Finding
	for _, path := range strings.Fields(string(out)) {
		if !strings.HasPrefix(path, "/org/freedesktop/UDisks2/drives/") {
			continue
		}
		var removable bool
		busProp(ctx, s, path, udisks+".Drive", "Removable", &removable)
		if removable {
			continue
		}
		var model string
		busProp(ctx, s, path, udisks+".Drive", "Model", &model)
		if f, ok := udisksNVMe(ctx, s, path, model); ok {
			fs = append(fs, f)
		} else if f, ok := udisksATA(ctx, s, path, model); ok {
			fs = append(fs, f)
		}
	}
	return fs, len(fs) > 0
}

func busProp(ctx context.Context, s sys.System, path, iface, prop string, v any) bool {
	out, err := s.Run(ctx, "busctl", "--json=short", "get-property", udisks, path, iface, prop)
	if err != nil {
		return false
	}
	var r struct {
		Data json.RawMessage `json:"data"`
	}
	return json.Unmarshal(out, &r) == nil && json.Unmarshal(r.Data, v) == nil
}

func udisksNVMe(ctx context.Context, s sys.System, path, model string) (doctor.Finding, bool) {
	out, err := s.Run(ctx, "busctl", "--json=short", "call", udisks, path, udisks+".NVMe.Controller", "SmartGetAttributes", "a{sv}", "0")
	if err != nil {
		return doctor.Finding{}, false
	}
	var r struct {
		Data []map[string]struct {
			Data json.RawMessage `json:"data"`
		} `json:"data"`
	}
	if json.Unmarshal(out, &r) != nil || len(r.Data) == 0 {
		return doctor.Finding{}, false
	}
	num := func(k string) int {
		var n int
		_ = json.Unmarshal(r.Data[0][k].Data, &n) // a missing attribute counts as 0
		return n
	}
	var warnings []string
	busProp(ctx, s, path, udisks+".NVMe.Controller", "SmartCriticalWarning", &warnings)
	rep := smartReport{
		Model:       model,
		SmartStatus: &smartStatus{Passed: len(warnings) == 0},
		NVMe: &nvmeHealth{
			PercentageUsed: num("percent_used"),
			MediaErrors:    num("media_errors"),
			AvailableSpare: num("avail_spare"),
		},
	}
	if len(warnings) > 0 {
		rep.NVMe.CriticalWarning = 1
	}
	f := judgeSmart("SSD", rep)
	if len(warnings) > 0 {
		f.Evidence = append(f.Evidence, "critical warnings: "+strings.Join(warnings, ", "))
	}
	return f, true
}

func udisksATA(ctx context.Context, s sys.System, path, model string) (doctor.Finding, bool) {
	var supported bool
	if !busProp(ctx, s, path, udisks+".Drive.Ata", "SmartSupported", &supported) || !supported {
		return doctor.Finding{}, false
	}
	var failing bool
	var bad int64
	busProp(ctx, s, path, udisks+".Drive.Ata", "SmartFailing", &failing)
	busProp(ctx, s, path, udisks+".Drive.Ata", "SmartNumBadSectors", &bad)
	rep := smartReport{Model: model, SmartStatus: &smartStatus{Passed: !failing}}
	if bad > 0 {
		attr := ataAttr{ID: 5}
		attr.Raw.Value = bad
		rep.ATA = &ataAttrs{Table: []ataAttr{attr}}
	}
	return judgeSmart("drive", rep), true
}
