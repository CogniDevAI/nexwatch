package collector

import "testing"

func TestIsVirtualDevice(t *testing.T) {
	tests := []struct {
		name string
		dev  string
		want bool
	}{
		{"loop device", "loop0", true},
		{"ram device", "ram1", true},
		{"device mapper device", "dm-0", true},
		{"physical disk sda", "sda", false},
		{"physical disk nvme", "nvme0n1", false},
		{"partition of a real disk", "sda1", false},
		{"empty name", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isVirtualDevice(tt.dev); got != tt.want {
				t.Errorf("isVirtualDevice(%q) = %v, want %v", tt.dev, got, tt.want)
			}
		})
	}
}
