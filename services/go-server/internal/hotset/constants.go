package hotset

// Traffic congestion and flood severity levels matching constants across the system.
const (
	SeverityClear          = "CLEAR"
	SeverityModerate       = "MODERATE"
	SeverityHeavy          = "HEAVY"
	SeverityCongested      = "CONGESTED"
	SeverityUnknown        = "UNKNOWN"
	SeverityDry            = "DRY"
	SeverityWet            = "WET"
	SeverityFloodedShallow = "FLOODED_SHALLOW"
	SeverityFloodedDeep    = "FLOODED_DEEP"
)

// API route paths for the hotset module.
const (
	RouteTrafficStatus = "/traffic-status"
	RouteFloodStatus   = "/flood-status"
	RouteHotsetStatus  = "/cameras/hotset/status"
)
