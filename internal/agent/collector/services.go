package collector

// ServicesCollector gathers OS service statuses: systemd units on Linux
// (services_linux.go), Windows services via the Service Control Manager
// (services_windows.go), and an "unsupported" empty result on any other
// platform (services_other.go). Collect's implementation is provided by
// exactly one of those build-tagged files; this file holds only the
// cross-platform type and constructor so both Name() and NewServicesCollector
// are defined once regardless of GOOS.
type ServicesCollector struct{}

// NewServicesCollector creates a new services collector.
func NewServicesCollector() *ServicesCollector {
	return &ServicesCollector{}
}

// Name returns the collector identifier.
func (c *ServicesCollector) Name() string { return "services" }
