package linux

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hpe-storage/common-host-libs/model"
)

func TestDeleteEmptyTarget(t *testing.T) {
	target := &model.IscsiTarget{
		Name:    "",
		Address: "172.10.10.10",
		Port:    "3260",
	}
	err := iscsiDeleteNode(target)
	if err.Error() != "Empty target to delete Node" {
		t.Error("empty target should not be allowed to be deleted")
	}
}

func TestGetIscsiHostNumbersForTargetIqns_EmptyTargets(t *testing.T) {
	hosts, err := GetIscsiHostNumbersForTargetIqns(nil)
	if err != nil {
		t.Errorf("expected no error for nil targets, got %v", err)
	}
	if len(hosts) != 0 {
		t.Errorf("expected empty hosts for nil targets, got %v", hosts)
	}

	hosts, err = GetIscsiHostNumbersForTargetIqns([]string{})
	if err != nil {
		t.Errorf("expected no error for empty targets, got %v", err)
	}
	if len(hosts) != 0 {
		t.Errorf("expected empty hosts for empty targets, got %v", hosts)
	}
}

func TestRescanIscsiHostsForLun_EmptyHosts(t *testing.T) {
	err := RescanIscsiHostsForLun([]string{}, "3")
	if err != nil {
		t.Errorf("expected no error for empty host list, got %v", err)
	}
}

func TestRescanIscsiHostsForLun_NonExistentHost(t *testing.T) {
	// host99999 should not exist on any system
	err := RescanIscsiHostsForLun([]string{"99999"}, "3")
	if err != nil {
		t.Errorf("expected no error for non-existent host (should skip), got %v", err)
	}
}

// TestGetIscsiHostNumbersForTargetIqns_MatchAndScope exercises the os.ReadDir
// session enumeration and target-IQN matching against a fake sysfs tree.
func TestGetIscsiHostNumbersForTargetIqns_MatchAndScope(t *testing.T) {
	scsiRoot := t.TempDir()
	iscsiRoot := t.TempDir()
	origScsi, origIscsi := scsiHostBasePath, iscsiHostRootPath
	scsiHostBasePath = scsiRoot
	iscsiHostRootPath = iscsiRoot
	defer func() { scsiHostBasePath, iscsiHostRootPath = origScsi, origIscsi }()

	// Host enumeration source: /sys/class/iscsi_host/host<N>
	if err := os.MkdirAll(filepath.Join(iscsiRoot, "host33"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Session/target source: /sys/class/scsi_host/host<N>/device/session<S>/iscsi_session/session<S>/targetname
	writeTarget := func(host, sess, iqn string) {
		dir := filepath.Join(scsiRoot, "host"+host, "device", "session"+sess, "iscsi_session", "session"+sess)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "targetname"), []byte(iqn+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeTarget("33", "10", "iqn.2023-24.com.hpe:target1")
	// A stray non-session dir under device/ that must be ignored.
	if err := os.MkdirAll(filepath.Join(scsiRoot, "host33", "device", "power"), 0o755); err != nil {
		t.Fatal(err)
	}

	hosts, err := GetIscsiHostNumbersForTargetIqns([]string{"iqn.2023-24.com.hpe:target1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hosts) != 1 || hosts[0] != "33" {
		t.Fatalf("expected host [33], got %v", hosts)
	}

	// Non-matching IQN -> (nil, nil), caller falls back to full rescan.
	hosts, err = GetIscsiHostNumbersForTargetIqns([]string{"iqn.2023-24.com.hpe:other"})
	if err != nil {
		t.Fatalf("unexpected error for no-match: %v", err)
	}
	if len(hosts) != 0 {
		t.Fatalf("expected no hosts for no-match, got %v", hosts)
	}
}

// TestFilterEmptyTargets verifies that empty-string entries are stripped while
// non-empty target IQNs are preserved in order. This gates the APP-failover
// primary-skip decision in HandleIscsiDiscovery, where model.Volume.TargetNames()
// returns []string{""} for a volume with no target IQNs.
func TestFilterEmptyTargets(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{
			name:  "nil slice",
			input: nil,
			want:  nil,
		},
		{
			name:  "empty slice",
			input: []string{},
			want:  nil,
		},
		{
			name:  "single empty string (TargetNames of empty volume)",
			input: []string{""},
			want:  nil,
		},
		{
			name:  "all empty strings",
			input: []string{"", "", ""},
			want:  nil,
		},
		{
			name:  "single valid target",
			input: []string{"iqn.2023-24.com.hpe:target1"},
			want:  []string{"iqn.2023-24.com.hpe:target1"},
		},
		{
			name:  "mixed empty and valid preserves order",
			input: []string{"", "iqn.2023-24.com.hpe:target1", "", "iqn.2023-24.com.hpe:target2"},
			want:  []string{"iqn.2023-24.com.hpe:target1", "iqn.2023-24.com.hpe:target2"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := filterEmptyTargets(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("filterEmptyTargets(%v) = %v, want %v", tc.input, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("filterEmptyTargets(%v)[%d] = %q, want %q", tc.input, i, got[i], tc.want[i])
				}
			}
		})
	}
}

