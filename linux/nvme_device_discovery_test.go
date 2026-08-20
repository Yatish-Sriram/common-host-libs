// Copyright 2026 Hewlett Packard Enterprise Development LP

package linux

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hpe-storage/common-host-libs/model"
)

// fakeFileInfo is a minimal os.FileInfo whose only meaningful field is Name(),
// which is all GetNvmeDeviceFromNamespace consumes when iterating /dev entries.
type fakeFileInfo struct{ name string }

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { return 0 }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any           { return nil }

// TestGetNvmeDeviceFromNamespace_SkipsUnreadableNguid is the ESC-17833 regression:
// the first NVMe namespace (e.g. a local boot drive like nvme0n1) has no readable
// nguid, and the target NVMe/TCP volume is on a later device (nvme1n1). Discovery
// must skip the unreadable device and still find the target instead of aborting.
func TestGetNvmeDeviceFromNamespace_SkipsUnreadableNguid(t *testing.T) {
	origReadDir := nvmeReadDir
	origReadNguid := readNvmeNamespaceNguid
	defer func() {
		nvmeReadDir = origReadDir
		readNvmeNamespaceNguid = origReadNguid
	}()

	const targetSerial = "60002ac00000003e0002ac940002b10c"

	nvmeReadDir = func(string) ([]os.FileInfo, error) {
		return []os.FileInfo{
			fakeFileInfo{name: "nvme0n1"}, // local boot drive: nguid read fails
			fakeFileInfo{name: "nvme1n1"}, // attached NVMe/TCP volume: matches
		}, nil
	}
	readNvmeNamespaceNguid = func(deviceName string) (string, error) {
		if deviceName == "nvme0n1" {
			return "", fmt.Errorf("open /sys/class/block/nvme0n1/subsystem/nvme0n1/nguid: no such file or directory")
		}
		// Return the (dashed) sysfs form; the function normalizes it.
		return "60002ac0-0000-003e-0002-ac940002b10c", nil
	}

	dev, err := GetNvmeDeviceFromNamespace(targetSerial)
	if err != nil {
		t.Fatalf("expected to find device on nvme1n1 despite nvme0n1 nguid failure, got error: %v", err)
	}
	if dev == nil {
		t.Fatalf("expected a device, got nil")
	}
	if dev.SerialNumber != targetSerial {
		t.Fatalf("expected serial %s, got %s", targetSerial, dev.SerialNumber)
	}
	if dev.Pathname != "/dev/nvme1n1" {
		t.Fatalf("expected /dev/nvme1n1, got %s", dev.Pathname)
	}
}

// TestGetNvmeDeviceFromNamespace_NotFound verifies a clean not-found error when no
// device matches (and one namespace has an unreadable nguid).
func TestGetNvmeDeviceFromNamespace_NotFound(t *testing.T) {
	origReadDir := nvmeReadDir
	origReadNguid := readNvmeNamespaceNguid
	defer func() {
		nvmeReadDir = origReadDir
		readNvmeNamespaceNguid = origReadNguid
	}()

	nvmeReadDir = func(string) ([]os.FileInfo, error) {
		return []os.FileInfo{
			fakeFileInfo{name: "nvme0n1"},
			fakeFileInfo{name: "nvme1n1"},
		}, nil
	}
	readNvmeNamespaceNguid = func(deviceName string) (string, error) {
		if deviceName == "nvme0n1" {
			return "", fmt.Errorf("no such file or directory")
		}
		return "aaaa-bbbb", nil
	}

	dev, err := GetNvmeDeviceFromNamespace("does-not-match")
	if err == nil {
		t.Fatalf("expected not-found error, got device %+v", dev)
	}
	if dev != nil {
		t.Fatalf("expected nil device, got %+v", dev)
	}
}

func setNvmeOptimizationTestSeams(t *testing.T) {
	t.Helper()
	origReadDir := nvmeControllerReadDir
	origReadFirstLine := nvmeControllerReadFirstLine
	origExec := nvmeExecCommandOutput
	origFindDevices := nvmeFindDevices
	origApplyTuning := nvmeApplyTcpTuning
	t.Cleanup(func() {
		nvmeControllerReadDir = origReadDir
		nvmeControllerReadFirstLine = origReadFirstLine
		nvmeExecCommandOutput = origExec
		nvmeFindDevices = origFindDevices
		nvmeApplyTcpTuning = origApplyTuning
	})
}

func TestGetLiveNvmeControllersForNQN(t *testing.T) {
	setNvmeOptimizationTestSeams(t)
	nvmeControllerReadDir = func(string) ([]os.FileInfo, error) {
		return []os.FileInfo{
			fakeFileInfo{name: "nvme0"},
			fakeFileInfo{name: "nvme1"},
			fakeFileInfo{name: "nvme2"},
		}, nil
	}
	nvmeControllerReadFirstLine = func(path string) (string, error) {
		values := map[string]string{
			"/sys/class/nvme/nvme0/subsysnqn": "nqn.target",
			"/sys/class/nvme/nvme0/state":     "live",
			"/sys/class/nvme/nvme0/address":   "traddr=10.0.0.1,trsvcid=4420",
			"/sys/class/nvme/nvme1/subsysnqn": "nqn.target",
			"/sys/class/nvme/nvme1/state":     "connecting",
			"/sys/class/nvme/nvme1/address":   "traddr=10.0.0.2,trsvcid=4420",
			"/sys/class/nvme/nvme2/subsysnqn": "nqn.other",
		}
		value, ok := values[path]
		if !ok {
			return "", fmt.Errorf("unexpected path %s", path)
		}
		return value, nil
	}

	addrs, err := getLiveNvmeControllersForNQN("nqn.target")
	if err != nil {
		t.Fatalf("getLiveNvmeControllersForNQN() error = %v", err)
	}
	if len(addrs) != 1 || !addrs["10.0.0.1"] {
		t.Fatalf("live addresses = %v, want only 10.0.0.1", addrs)
	}
}

func TestGetLiveNvmeControllersForNQNReadDirError(t *testing.T) {
	setNvmeOptimizationTestSeams(t)
	nvmeControllerReadDir = func(string) ([]os.FileInfo, error) {
		return nil, fmt.Errorf("sysfs unavailable")
	}

	addrs, err := getLiveNvmeControllersForNQN("nqn.target")
	if err == nil {
		t.Fatal("getLiveNvmeControllersForNQN() error = nil, want error")
	}
	if len(addrs) != 0 {
		t.Fatalf("live addresses = %v, want empty", addrs)
	}
}

func TestAllTargetPortalsLive(t *testing.T) {
	volume := &model.Volume{TargetAddress: " 10.0.0.1,10.0.0.2 "}
	if !allTargetPortalsLive(volume, map[string]bool{"10.0.0.1": true, "10.0.0.2": true}) {
		t.Fatal("allTargetPortalsLive() = false, want true")
	}
	if allTargetPortalsLive(volume, map[string]bool{"10.0.0.1": true}) {
		t.Fatal("allTargetPortalsLive() = true with missing portal, want false")
	}
}

func TestDiscoverNvmeTargetDeduplicatesEndpoints(t *testing.T) {
	setNvmeOptimizationTestSeams(t)
	nvmeExecCommandOutput = func(_ string, args []string) (string, int, error) {
		if args[0] == "disconnect" {
			return "", 0, nil
		}
		return "=====Discovery Log Entry 0=====\nsubnqn: nqn.target\ntraddr: 10.0.0.1\ntrsvcid: 4420\n=====Discovery Log Entry 1=====\nsubnqn: nqn.target\ntraddr: 10.0.0.1\ntrsvcid: 4420\n=====Discovery Log Entry 2=====\nsubnqn: nqn.target\ntraddr: 10.0.0.2\ntrsvcid: 4420\n", 0, nil
	}

	target := discoverNvmeTarget(&model.Volume{Nqn: "nqn.target", DiscoveryIPs: []string{"10.0.0.10"}})
	if target == nil {
		t.Fatal("discoverNvmeTarget() = nil, want target")
	}
	if target.Address != "10.0.0.1,10.0.0.2" || target.Port != "4420" {
		t.Fatalf("discovered target = %+v, want addresses 10.0.0.1,10.0.0.2 and port 4420", target)
	}
}

func TestConnectNvmeTargetSkipsLivePortal(t *testing.T) {
	setNvmeOptimizationTestSeams(t)
	nvmeControllerReadDir = func(string) ([]os.FileInfo, error) {
		return []os.FileInfo{fakeFileInfo{name: "nvme0"}}, nil
	}
	nvmeControllerReadFirstLine = func(path string) (string, error) {
		values := map[string]string{
			"/sys/class/nvme/nvme0/subsysnqn": "nqn.target",
			"/sys/class/nvme/nvme0/state":     "live",
			"/sys/class/nvme/nvme0/address":   "traddr=10.0.0.1,trsvcid=4420",
		}
		return values[path], nil
	}
	var connectAddresses []string
	nvmeExecCommandOutput = func(_ string, args []string) (string, int, error) {
		connectAddresses = append(connectAddresses, args[6])
		return "", 0, nil
	}

	err := ConnectNvmeTarget(&model.NvmeTarget{NQN: "nqn.target", Address: "10.0.0.1,10.0.0.2", Port: "4420"})
	if err != nil {
		t.Fatalf("ConnectNvmeTarget() error = %v", err)
	}
	if len(connectAddresses) != 1 || connectAddresses[0] != "10.0.0.2" {
		t.Fatalf("connect addresses = %v, want only 10.0.0.2", connectAddresses)
	}
}

func TestHandleNvmeTcpDiscoveryConnectsMissingPortalWhenPartiallyLive(t *testing.T) {
	setNvmeOptimizationTestSeams(t)
	nvmeControllerReadDir = func(string) ([]os.FileInfo, error) {
		return []os.FileInfo{fakeFileInfo{name: "nvme0"}}, nil
	}
	nvmeControllerReadFirstLine = func(path string) (string, error) {
		values := map[string]string{
			"/sys/class/nvme/nvme0/subsysnqn": "nqn.target",
			"/sys/class/nvme/nvme0/state":     "live",
			"/sys/class/nvme/nvme0/address":   "traddr=10.0.0.1,trsvcid=4420",
		}
		return values[path], nil
	}
	nvmeApplyTcpTuning = func() error {
		return nil
	}
	var commands []string
	var connectedAddress string
	nvmeExecCommandOutput = func(_ string, args []string) (string, int, error) {
		commands = append(commands, args[0])
		if args[0] == "discover" {
			t.Fatal("nvme discover must not run when a controller is already live")
		}
		if args[0] == "connect" {
			connectedAddress = args[6]
		}
		return "", 0, nil
	}
	nvmeFindDevices = func(string) ([]string, error) {
		return []string{"/dev/nvme0n1"}, nil
	}

	err := HandleNvmeTcpDiscovery(&model.Volume{
		Nqn:           "nqn.target",
		SerialNumber:  "serial",
		TargetAddress: "10.0.0.1,10.0.0.2",
		TargetPort:    "4420",
	})
	if err != nil {
		t.Fatalf("HandleNvmeTcpDiscovery() error = %v", err)
	}
	if connectedAddress != "10.0.0.2" {
		t.Fatalf("connect address = %q, want missing portal 10.0.0.2", connectedAddress)
	}
	if fmt.Sprint(commands) != "[connect]" {
		t.Fatalf("commands = %v, want [connect]", commands)
	}
}

func TestHandleNvmeTcpDiscoveryFallsBackAfterControllerScanFailure(t *testing.T) {
	setNvmeOptimizationTestSeams(t)
	nvmeControllerReadDir = func(string) ([]os.FileInfo, error) {
		return nil, fmt.Errorf("sysfs unavailable")
	}
	nvmeApplyTcpTuning = func() error {
		return nil
	}
	var commands []string
	var connectedAddress string
	nvmeExecCommandOutput = func(_ string, args []string) (string, int, error) {
		commands = append(commands, args[0])
		switch args[0] {
		case "discover", "disconnect":
			return "", 0, nil
		case "connect":
			connectedAddress = args[6]
			return "", 0, nil
		default:
			return "", 1, fmt.Errorf("unexpected command %v", args)
		}
	}
	nvmeFindDevices = func(string) ([]string, error) {
		return []string{"/dev/nvme0n1"}, nil
	}

	err := HandleNvmeTcpDiscovery(&model.Volume{
		Nqn:           "nqn.target",
		SerialNumber:  "serial",
		TargetAddress: "10.0.0.1",
		TargetPort:    "4420",
		DiscoveryIPs:  []string{"10.0.0.10"},
	})
	if err != nil {
		t.Fatalf("HandleNvmeTcpDiscovery() error = %v", err)
	}
	if connectedAddress != "10.0.0.1" {
		t.Fatalf("connect address = %q, want CSP fallback 10.0.0.1", connectedAddress)
	}
	wantCommands := []string{"discover", "disconnect", "connect"}
	if fmt.Sprint(commands) != fmt.Sprint(wantCommands) {
		t.Fatalf("commands = %v, want %v", commands, wantCommands)
	}
}

func TestHandleNvmeTcpDiscoverySkipsFullyStagedVolume(t *testing.T) {
	setNvmeOptimizationTestSeams(t)
	nvmeControllerReadDir = func(string) ([]os.FileInfo, error) {
		return []os.FileInfo{fakeFileInfo{name: "nvme0"}}, nil
	}
	nvmeControllerReadFirstLine = func(path string) (string, error) {
		values := map[string]string{
			"/sys/class/nvme/nvme0/subsysnqn": "nqn.target",
			"/sys/class/nvme/nvme0/state":     "live",
			"/sys/class/nvme/nvme0/address":   "traddr=10.0.0.1,trsvcid=4420",
		}
		return values[path], nil
	}
	nvmeFindDevices = func(string) ([]string, error) {
		return []string{"/dev/nvme0n1"}, nil
	}
	nvmeApplyTcpTuning = func() error {
		return nil
	}
	nvmeExecCommandOutput = func(string, []string) (string, int, error) {
		t.Fatal("nvme command must not run for a fully staged volume")
		return "", 0, nil
	}

	err := HandleNvmeTcpDiscovery(&model.Volume{
		Nqn:           "nqn.target",
		SerialNumber:  "serial",
		TargetAddress: "10.0.0.1",
		TargetPort:    "4420",
	})
	if err != nil {
		t.Fatalf("HandleNvmeTcpDiscovery() error = %v", err)
	}
}
