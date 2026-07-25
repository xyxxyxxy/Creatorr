package web

// PresetOption is one chip under a preset_fill text field (Value written into the input).
type PresetOption struct {
	Value string
	Label string
}

// DomainPresets builds preset options from source hostnames.
func DomainPresets(domains []string) []PresetOption {
	out := make([]PresetOption, 0, len(domains))
	for _, d := range domains {
		if d == "" {
			continue
		}
		out = append(out, PresetOption{Value: d, Label: d})
	}
	return out
}
