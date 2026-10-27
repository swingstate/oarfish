package metrics

import "time"

// History is the recent course of three values for the sparklines, oldest
// point first. A point that could not be read is null. All three series have
// the same length. It lives in memory only and is empty while nobody watches.
type History struct {
	SchemaVersion      int        `json:"schema_version"`
	IntervalSeconds    float64    `json:"interval_seconds"`
	CPUPercent         []*float64 `json:"cpu_percent"`
	MemoryUsedPercent  []*float64 `json:"memory_used_percent"`
	TemperatureCelsius []*float64 `json:"temperature_celsius"` // the hottest CPU sensor
}

type ring struct {
	size           int
	cpu, mem, temp []*float64
}

func newRing(size int) ring { return ring{size: size} }

func (r *ring) reset() { r.cpu, r.mem, r.temp = nil, nil, nil }

func (r *ring) add(s Snapshot) {
	r.cpu = push(r.cpu, s.CPU.UsagePercent, r.size)
	var mem *float64
	if m := s.Memory; m.TotalBytes != nil && m.UsedBytes != nil && *m.TotalBytes > 0 {
		v := round(float64(*m.UsedBytes)/float64(*m.TotalBytes)*100, 1)
		mem = &v
	}
	r.mem = push(r.mem, mem, r.size)
	var temp *float64
	if t, ok := hottestCPU(s.Temperatures); ok {
		temp = &t
	}
	r.temp = push(r.temp, temp, r.size)
}

// push appends v and drops the oldest point beyond size. It copies into a new
// slice once the ring is full so the backing array does not grow without bound.
func push(series []*float64, v *float64, size int) []*float64 {
	if len(series) >= size {
		copy(series, series[1:])
		series = series[:size-1]
	}
	return append(series, v)
}

func (r *ring) history(interval time.Duration) History {
	cp := func(in []*float64) []*float64 {
		out := make([]*float64, len(in))
		copy(out, in)
		return out
	}
	return History{
		SchemaVersion: SchemaVersion, IntervalSeconds: interval.Seconds(),
		CPUPercent: cp(r.cpu), MemoryUsedPercent: cp(r.mem), TemperatureCelsius: cp(r.temp),
	}
}
