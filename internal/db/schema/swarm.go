package schema

// HealthCheckSwarm represents Docker Swarm health check configuration.
type HealthCheckSwarm struct {
	Test        []string `json:"Test,omitempty"`
	Interval    *int64   `json:"Interval,omitempty"`
	Timeout     *int64   `json:"Timeout,omitempty"`
	StartPeriod *int64   `json:"StartPeriod,omitempty"`
	Retries     *int     `json:"Retries,omitempty"`
}

// RestartPolicySwarm represents Docker Swarm restart policy.
type RestartPolicySwarm struct {
	Condition   *string `json:"Condition,omitempty"`
	Delay       *int64  `json:"Delay,omitempty"`
	MaxAttempts *int    `json:"MaxAttempts,omitempty"`
	Window      *int64  `json:"Window,omitempty"`
}

// PlacementSwarm represents Docker Swarm placement constraints.
type PlacementSwarm struct {
	Constraints []string              `json:"Constraints,omitempty"`
	Preferences []PlacementPreference `json:"Preferences,omitempty"`
	MaxReplicas *int                  `json:"MaxReplicas,omitempty"`
	Platforms   []Platform            `json:"Platforms,omitempty"`
}

// PlacementPreference represents a placement preference.
type PlacementPreference struct {
	Spread struct {
		SpreadDescriptor string `json:"SpreadDescriptor"`
	} `json:"Spread"`
}

// Platform represents a platform constraint.
type Platform struct {
	Architecture string `json:"Architecture"`
	OS           string `json:"OS"`
}

// UpdateConfigSwarm represents Docker Swarm update/rollback configuration.
type UpdateConfigSwarm struct {
	Parallelism     int     `json:"Parallelism"`
	Delay           *int64  `json:"Delay,omitempty"`
	FailureAction   *string `json:"FailureAction,omitempty"`
	Monitor         *int64  `json:"Monitor,omitempty"`
	MaxFailureRatio *float64 `json:"MaxFailureRatio,omitempty"`
	Order           string  `json:"Order"`
}

// ServiceModeSwarm represents Docker Swarm service mode.
type ServiceModeSwarm struct {
	Replicated    *ReplicatedMode    `json:"Replicated,omitempty"`
	Global        *struct{}          `json:"Global,omitempty"`
	ReplicatedJob *ReplicatedJobMode `json:"ReplicatedJob,omitempty"`
	GlobalJob     *struct{}          `json:"GlobalJob,omitempty"`
}

// ReplicatedMode for replicated services.
type ReplicatedMode struct {
	Replicas *int `json:"Replicas,omitempty"`
}

// ReplicatedJobMode for replicated job services.
type ReplicatedJobMode struct {
	MaxConcurrent    *int `json:"MaxConcurrent,omitempty"`
	TotalCompletions *int `json:"TotalCompletions,omitempty"`
}

// NetworkSwarm represents Docker Swarm network attachment.
type NetworkSwarm struct {
	Target     *string           `json:"Target,omitempty"`
	Aliases    []string          `json:"Aliases,omitempty"`
	DriverOpts map[string]string `json:"DriverOpts,omitempty"`
}

// LabelsSwarm is a map of Docker Swarm labels.
type LabelsSwarm map[string]string

// EndpointSpecSwarm represents Docker Swarm endpoint configuration.
type EndpointSpecSwarm struct {
	Mode  *string                   `json:"Mode,omitempty"`
	Ports []EndpointPortConfigSwarm `json:"Ports,omitempty"`
}

// EndpointPortConfigSwarm represents a port configuration in endpoint spec.
type EndpointPortConfigSwarm struct {
	Protocol      *string `json:"Protocol,omitempty"`
	TargetPort    *int    `json:"TargetPort,omitempty"`
	PublishedPort *int    `json:"PublishedPort,omitempty"`
	PublishMode   *string `json:"PublishMode,omitempty"`
}

// UlimitSwarm represents a single ulimit configuration.
type UlimitSwarm struct {
	Name string `json:"Name"`
	Soft int    `json:"Soft"`
	Hard int    `json:"Hard"`
}

// UlimitsSwarm is a list of ulimit configurations.
type UlimitsSwarm []UlimitSwarm
