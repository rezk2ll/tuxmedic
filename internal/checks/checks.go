// Package checks holds every diagnosis tuxmedic can run.
package checks

import "github.com/rezk2ll/tuxmedic/internal/doctor"

func All() []doctor.Check {
	return []doctor.Check{
		Crash, Network, Sound, Battery, Disk, Memory, Thermal,
		Drivers, Sleep, Bluetooth, Updates, Boot, Clock,
	}
}
