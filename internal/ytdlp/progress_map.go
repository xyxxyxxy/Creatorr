package ytdlp

// ProgressMapper maps yt-dlp per-format 0..1 fractions into a monotonic
// overall [Lo, Hi] span. Separate video/audio (and similar) downloads each
// report 0→100%; without mapping the task bar resets mid-download.
type ProgressMapper struct {
	Lo, Hi float64

	parts int     // completed format downloads (resets seen)
	last  float64 // last raw fraction
	out   float64 // last mapped output (never decreases)
	init  bool
}

// Map converts a raw 0..1 yt-dlp fraction into overall progress in [Lo, Hi].
func (m *ProgressMapper) Map(raw float64) float64 {
	if m.Hi <= m.Lo {
		m.Hi = m.Lo + 0.01
	}
	if raw < 0 {
		raw = 0
	}
	if raw > 1 {
		raw = 1
	}
	if !m.init {
		m.init = true
		m.last = raw
		m.out = m.Lo + (m.Hi-m.Lo)*raw
		return m.out
	}
	// New format file: percent drops sharply after a high watermark.
	if raw+0.2 < m.last && m.last > 0.5 {
		m.parts++
	}
	m.last = raw
	slots := float64(m.parts + 1)
	v := m.Lo + (m.Hi-m.Lo)*((float64(m.parts)+raw)/slots)
	if v < m.out {
		v = m.out
	}
	if v > m.Hi {
		v = m.Hi
	}
	m.out = v
	return v
}

// Finish returns Hi (call when the download phase completed successfully).
func (m *ProgressMapper) Finish() float64 {
	if m.Hi <= m.Lo {
		return m.Lo
	}
	m.out = m.Hi
	return m.out
}
