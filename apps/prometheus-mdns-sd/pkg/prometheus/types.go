package prometheus

// TargetGroup represents a Prometheus service discovery target group
type TargetGroup struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels,omitempty"`
}

// TargetGroupList is a list of target groups for Prometheus service discovery
type TargetGroupList []TargetGroup
