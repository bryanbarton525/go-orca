package state

import "encoding/json"

// DesignComponentList unmarshals architect "components" arrays that may mix
// full objects with shorthand strings (e.g. "rate-limiter").
type DesignComponentList []DesignComponent

func (d *DesignComponentList) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	out := make([]DesignComponent, 0, len(raw))
	for _, item := range raw {
		var comp DesignComponent
		if err := json.Unmarshal(item, &comp); err == nil && comp.Name != "" {
			out = append(out, comp)
			continue
		}
		var name string
		if err := json.Unmarshal(item, &name); err == nil && name != "" {
			out = append(out, DesignComponent{Name: name, Description: name})
		}
	}
	*d = out
	return nil
}
