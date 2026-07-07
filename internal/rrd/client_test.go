package rrd

import (
	"testing"
	"time"
)

func TestSanitizeAndRewritePath(t *testing.T) {
	client := NewCLIClient("rrdtool", "/var/lib/cacti/rra", 5*time.Second, 10)

	tests := []struct {
		input   string
		valid   bool
		expected string
	}{
		{"localhost_mem_buffers_3.rrd", true, "/var/lib/cacti/rra/localhost_mem_buffers_3.rrd"},
		{"sub/localhost_mem_buffers_3.rrd", true, "/var/lib/cacti/rra/sub/localhost_mem_buffers_3.rrd"},
		{"../localhost_mem_buffers_3.rrd", false, ""},
		{"/etc/passwd", false, ""},
		{"localhost_mem_buffers_3.txt", false, ""},
		{"localhost_mem_buffers_3.RRD", true, "/var/lib/cacti/rra/localhost_mem_buffers_3.RRD"},
		{"some_dir/../other_dir/file.rrd", true, "/var/lib/cacti/rra/other_dir/file.rrd"},
		{"some_dir/../../other_dir/file.rrd", false, ""},
	}

	for _, tc := range tests {
		result, err := client.SanitizeAndRewritePath(tc.input)
		if tc.valid {
			if err != nil {
				t.Errorf("Expected path %q to be valid, but got error: %v", tc.input, err)
			}
			if result != tc.expected {
				t.Errorf("Expected path %q to resolve to %q, but got %q", tc.input, tc.expected, result)
			}
		} else {
			if err == nil {
				t.Errorf("Expected path %q to be invalid, but got no error and result %q", tc.input, result)
			}
		}
	}
}

func TestRewriteSpecs(t *testing.T) {
	client := NewCLIClient("rrdtool", "/var/lib/cacti/rra", 5*time.Second, 10)

	specs := []string{
		"DEF:val=localhost_mem_buffers_3.rrd:mem_buffers:AVERAGE",
		"DEF:cpu=sub/cpu.rrd:cpu_system:AVERAGE",
		"CDEF:v2=val,8,*",
		"LINE1:val#FF0000:Legend",
	}

	expected := []string{
		"DEF:val=/var/lib/cacti/rra/localhost_mem_buffers_3.rrd:mem_buffers:AVERAGE",
		"DEF:cpu=/var/lib/cacti/rra/sub/cpu.rrd:cpu_system:AVERAGE",
		"CDEF:v2=val,8,*",
		"LINE1:val#FF0000:Legend",
	}

	rewritten, err := client.RewriteSpecs(specs)
	if err != nil {
		t.Fatalf("Failed to rewrite specs: %v", err)
	}

	for i, spec := range rewritten {
		if spec != expected[i] {
			t.Errorf("Index %d: expected %q, got %q", i, expected[i], spec)
		}
	}

	// Test with invalid RRD inside DEF
	invalidSpecs := []string{
		"DEF:val=../escape.rrd:ds:AVERAGE",
	}
	_, err = client.RewriteSpecs(invalidSpecs)
	if err == nil {
		t.Errorf("Expected failure for traversal attempt inside DEF statement")
	}
}
