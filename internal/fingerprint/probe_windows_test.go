// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build windows

package fingerprint

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"
)

func TestWindowsProbeSuccess(t *testing.T) {
	t.Parallel()
	api := newFakeWindowsAPI()
	target := probeWindows(context.Background(), `C:\safe\output`, Options{IncludeCPUPressure: true}, api)
	if err := target.Validate(); err != nil {
		t.Fatalf("Validate() error: %v\ntarget: %+v", err, target)
	}
	if target.Version != "10.0" || target.Build != "26100" || target.Architecture != "amd64" {
		t.Fatalf("unexpected identity: %+v", target)
	}
	if target.RuntimeLane != "" {
		t.Fatalf("native probe assigned runtime policy lane %q", target.RuntimeLane)
	}
	if target.Privilege != "filtered-admin" || target.Elevated {
		t.Fatalf("unexpected privilege context: privilege=%q elevated=%v", target.Privilege, target.Elevated)
	}
	if got := *target.Probe.CPUBusyBasisPoints.Value; got != 8333 {
		t.Fatalf("cpu busy basis points = %d, want 8333", got)
	}
	if len(api.waitDurations) != 1 || api.waitDurations[0] != cpuPressureSampleInterval {
		t.Fatalf("CPU sample waits = %v, want one %s wait", api.waitDurations, cpuPressureSampleInterval)
	}
	if got := *target.Probe.OutputVolume.ValidatedOutputPath.Value; got != `C:\safe\output` {
		t.Fatalf("validated output association = %q", got)
	}
}

func TestWindowsArchitectureAndCPUFallbacks(t *testing.T) {
	t.Parallel()
	api := newFakeWindowsAPI()
	api.wowErr = errAPIUnavailable
	api.activeProcessorErr = errAPIUnavailable
	api.info = systemInfo{ProcessorArchitecture: processorArchitectureARM64, NumberOfProcessors: 4}

	target := probeWindows(context.Background(), `C:\safe\output`, Options{}, api)
	if target.Architecture != "arm64" || target.Probe.NativeArchitecture.Source != "GetNativeSystemInfo" {
		t.Fatalf("architecture fallback failed: %+v", target.Probe.NativeArchitecture)
	}
	if target.Probe.LogicalProcessorCount.State != Known || *target.Probe.LogicalProcessorCount.Value != 4 || target.Probe.LogicalProcessorCount.Source != "GetNativeSystemInfo" {
		t.Fatalf("CPU fallback failed: %+v", target.Probe.LogicalProcessorCount)
	}
	if api.nativeSystemInfoCalls != 1 {
		t.Fatalf("GetNativeSystemInfo calls = %d, want one shared fallback", api.nativeSystemInfoCalls)
	}
}

func TestWindowsArchitectureCallFailureIsNotSilentlyMasked(t *testing.T) {
	t.Parallel()
	api := newFakeWindowsAPI()
	api.wowErr = &apiError{reason: "API_CALL_FAILED", code: 5}
	target := probeWindows(context.Background(), `C:\safe\output`, Options{}, api)
	if target.Probe.NativeArchitecture.State != Failed || target.Probe.NativeArchitecture.Value != nil {
		t.Fatalf("architecture failure was not preserved: %+v", target.Probe.NativeArchitecture)
	}
	if api.nativeSystemInfoCalls != 0 {
		t.Fatalf("unexpected fallback after API call failure: %d", api.nativeSystemInfoCalls)
	}
	if err := target.Validate(); err == nil {
		t.Fatal("target with unknown native architecture unexpectedly validated")
	}
}

func TestWindowsMemoryFailureDoesNotFabricateValues(t *testing.T) {
	t.Parallel()
	api := newFakeWindowsAPI()
	api.memoryErr = &apiError{reason: "API_CALL_FAILED", code: 8}
	target := probeWindows(context.Background(), `C:\safe\output`, Options{}, api)
	for name, field := range map[string]Field[uint64]{
		"total":     target.Probe.TotalPhysicalMemoryBytes,
		"available": target.Probe.AvailablePhysicalMemoryBytes,
	} {
		if field.State != Failed || field.Value != nil || field.ErrorCode != 8 {
			t.Fatalf("%s memory field = %+v", name, field)
		}
	}
	if err := target.Validate(); err != nil {
		t.Fatalf("soft memory failure invalidated target: %v", err)
	}
}

func TestWindowsTokenFailureIsHonestAndSoft(t *testing.T) {
	t.Parallel()
	api := newFakeWindowsAPI()
	api.tokenErr = &apiError{reason: "TOKEN_ELEVATION_QUERY_FAILED", code: 5}
	target := probeWindows(context.Background(), `C:\safe\output`, Options{}, api)
	if target.Probe.Elevated.State != Failed || target.Probe.Elevated.Value != nil || target.Probe.TokenElevationType.Value != nil {
		t.Fatalf("token failure was not preserved: %+v %+v", target.Probe.Elevated, target.Probe.TokenElevationType)
	}
	if target.Privilege != "" {
		t.Fatalf("token failure fabricated privilege mirror %q", target.Privilege)
	}
	if err := target.Validate(); err != nil {
		t.Fatalf("soft token failure invalidated base fingerprint: %v", err)
	}
}

func TestWindowsUnsupportedTokenProbeIsSoft(t *testing.T) {
	t.Parallel()
	api := newFakeWindowsAPI()
	api.tokenErr = errAPIUnavailable
	target := probeWindows(context.Background(), `C:\safe\output`, Options{}, api)
	if target.Probe.Elevated.State != Unsupported || target.Probe.TokenElevationType.State != Unsupported {
		t.Fatalf("unsupported token state was not preserved: %+v %+v", target.Probe.Elevated, target.Probe.TokenElevationType)
	}
	if err := target.Validate(); err != nil {
		t.Fatalf("unsupported soft token fields invalidated base fingerprint: %v", err)
	}
}

func TestWindowsOutputVolumeFailuresRemainFieldScoped(t *testing.T) {
	t.Parallel()
	api := newFakeWindowsAPI()
	api.driveTypeErr = &apiError{reason: "API_CALL_FAILED", code: 21}
	api.fileSystemErr = &apiError{reason: "API_CALL_FAILED", code: 21}
	api.availableBytesErr = &apiError{reason: "API_CALL_FAILED", code: 21}
	target := probeWindows(context.Background(), `D:\validated\future-package`, Options{}, api)

	if target.Probe.OutputVolume.ValidatedOutputPath.State != Known || *target.Probe.OutputVolume.ValidatedOutputPath.Value != `D:\validated\future-package` {
		t.Fatalf("output path association was lost: %+v", target.Probe.OutputVolume.ValidatedOutputPath)
	}
	if target.Probe.OutputVolume.VolumeRoot.State != Known || *target.Probe.OutputVolume.VolumeRoot.Value != `D:\` {
		t.Fatalf("volume root association was lost: %+v", target.Probe.OutputVolume.VolumeRoot)
	}
	for name, state := range map[string]FieldState{
		"drive":      target.Probe.OutputVolume.DriveType.State,
		"filesystem": target.Probe.OutputVolume.FileSystem.State,
		"free":       target.Probe.OutputVolume.AvailableBytesCaller.State,
	} {
		if state != Failed {
			t.Fatalf("%s state = %s, want FAILED", name, state)
		}
	}
	if err := target.Validate(); err != nil {
		t.Fatalf("field-scoped volume failure invalidated base fingerprint: %v", err)
	}
	if api.volumeQueriesWithoutGuard {
		t.Fatal("volume query ran without SEM_FAILCRITICALERRORS")
	}
}

func TestWindowsVolumeQueriesPreserveThreadErrorMode(t *testing.T) {
	t.Parallel()
	for name, oldMode := range map[string]uint32{
		"zero default mode": 0,
		"existing flags":    0x0002,
	} {
		t.Run(name, func(t *testing.T) {
			api := newFakeWindowsAPI()
			api.threadErrorModeValue = oldMode
			target := probeWindows(context.Background(), `C:\safe\output`, Options{}, api)
			if err := target.Validate(); err != nil {
				t.Fatalf("Validate() error: %v", err)
			}
			if api.volumeQueriesWithoutGuard || api.volumeQueries != 3 {
				t.Fatalf("unexpected guarded volume query state: queries=%d unguarded=%v", api.volumeQueries, api.volumeQueriesWithoutGuard)
			}
			if got, want := api.threadErrorModeSets, []uint32{oldMode | semFailCriticalErrors, oldMode}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
				t.Fatalf("thread error mode transitions = %v, want %v", got, want)
			}
			if api.threadErrorModeValue != oldMode {
				t.Fatalf("thread error mode was not restored: %#x", api.threadErrorModeValue)
			}
			for name, state := range map[string]FieldState{
				"drive":      target.Probe.OutputVolume.DriveType.State,
				"filesystem": target.Probe.OutputVolume.FileSystem.State,
				"free":       target.Probe.OutputVolume.AvailableBytesCaller.State,
			} {
				if state != Known {
					t.Fatalf("normal guarded %s state = %s, want KNOWN", name, state)
				}
			}
		})
	}
}

func TestGuardedVolumeWorkerThreadContainment(t *testing.T) {
	t.Parallel()

	t.Run("restoration success unlocks exactly once", func(t *testing.T) {
		api := newFakeWindowsAPI()
		api.threadErrorModeValue = 0x0002
		lifecycle := &fakeThreadLifecycle{}
		drive, fileSystem, availableBytes, err := probeGuardedVolumeFactsWithThreadLifecycle(api, `C:\`, lifecycle)
		if err != nil {
			t.Fatalf("probeGuardedVolumeFactsWithThreadLifecycle() error: %v", err)
		}
		if lifecycle.lockCalls != 1 || lifecycle.unlockCalls != 1 {
			t.Fatalf("thread lifecycle calls = lock:%d unlock:%d, want lock:1 unlock:1", lifecycle.lockCalls, lifecycle.unlockCalls)
		}
		if api.threadErrorModeValue != 0x0002 {
			t.Fatalf("thread error mode = %#x, want exact restoration", api.threadErrorModeValue)
		}
		if got, want := api.threadErrorModeSets, []uint32{0x0002 | semFailCriticalErrors, 0x0002}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("thread error mode transitions = %v, want %v", got, want)
		}
		for name, field := range map[string]FieldState{
			"drive":      drive.State,
			"filesystem": fileSystem.State,
			"free":       availableBytes.State,
		} {
			if field != Known {
				t.Fatalf("%s state = %s, want KNOWN", name, field)
			}
		}
	})

	t.Run("restore failure publishes without unlock", func(t *testing.T) {
		api := newFakeWindowsAPI()
		api.threadErrorModeRestoreErr = &apiError{reason: "RESTORE_THREAD_ERROR_MODE_FAILED", code: 5}
		lifecycle := &fakeThreadLifecycle{}
		results, workerDone := startGuardedVolumeWorker(api, `C:\`, lifecycle)

		select {
		case <-workerDone:
		case <-time.After(time.Second):
			t.Fatal("restore-failed worker did not finish before its buffered result was received")
		}
		result := <-results
		if result.guardErr == nil || result.guardErr.Error() == "" {
			t.Fatalf("restore failure result = %+v, want explicit guard error", result)
		}
		if lifecycle.lockCalls != 1 || lifecycle.unlockCalls != 0 {
			t.Fatalf("thread lifecycle calls = lock:%d unlock:%d, want lock:1 unlock:0", lifecycle.lockCalls, lifecycle.unlockCalls)
		}
		if api.volumeQueries != 3 || api.volumeQueriesWithoutGuard {
			t.Fatalf("unexpected guarded volume query state: queries=%d unguarded=%v", api.volumeQueries, api.volumeQueriesWithoutGuard)
		}
	})

	t.Run("setup failure skips volume queries and unlocks clean thread", func(t *testing.T) {
		api := newFakeWindowsAPI()
		api.threadErrorModeSetErr = &apiError{reason: "SET_THREAD_ERROR_MODE_FAILED", code: 5}
		lifecycle := &fakeThreadLifecycle{}
		_, _, _, err := probeGuardedVolumeFactsWithThreadLifecycle(api, `C:\`, lifecycle)
		if err == nil {
			t.Fatal("guard setup failure was not returned")
		}
		if lifecycle.lockCalls != 1 || lifecycle.unlockCalls != 1 {
			t.Fatalf("thread lifecycle calls = lock:%d unlock:%d, want lock:1 unlock:1", lifecycle.lockCalls, lifecycle.unlockCalls)
		}
		if api.volumeQueries != 0 {
			t.Fatalf("volume queries = %d, want none after guard setup failure", api.volumeQueries)
		}
	})
}

func TestThreadErrorModeRawReturnSemantics(t *testing.T) {
	t.Parallel()
	if mode := threadErrorModeFromRawResult(0, syscall.Errno(5)); mode != 0 {
		t.Fatalf("GetThreadErrorMode mode = %#x, want valid zero", mode)
	}
	if err := setThreadErrorModeFromRawResult(1, syscall.Errno(5)); err != nil {
		t.Fatalf("successful SetThreadErrorMode rejected stale last error: %v", err)
	}
	err := setThreadErrorModeFromRawResult(0, syscall.Errno(5))
	var probeErr *apiError
	if !errors.As(err, &probeErr) || probeErr.reason != "SET_THREAD_ERROR_MODE_FAILED" || probeErr.code != 5 {
		t.Fatalf("failed SetThreadErrorMode error = %#v, want stable Win32 failure", err)
	}
}

func TestWindowsVolumeGuardFailuresPreventOrSurfaceQueries(t *testing.T) {
	t.Parallel()
	t.Run("setup failure prevents queries", func(t *testing.T) {
		api := newFakeWindowsAPI()
		api.threadErrorModeSetErr = &apiError{reason: "SET_THREAD_ERROR_MODE_FAILED", code: 5}
		target := probeWindows(context.Background(), `C:\safe\output`, Options{}, api)
		if api.volumeQueries != 0 {
			t.Fatalf("volume queries = %d, want none after guard setup failure", api.volumeQueries)
		}
		for name, field := range map[string]Field[string]{
			"drive":      target.Probe.OutputVolume.DriveType,
			"filesystem": target.Probe.OutputVolume.FileSystem,
		} {
			if field.State != Failed || field.Value != nil || field.ErrorReason != "SET_THREAD_ERROR_MODE_FAILED" {
				t.Fatalf("%s guard failure = %+v", name, field)
			}
		}
	})
	t.Run("unavailable guard prevents queries", func(t *testing.T) {
		api := newFakeWindowsAPI()
		api.threadErrorModeGetErr = errAPIUnavailable
		target := probeWindows(context.Background(), `C:\safe\output`, Options{}, api)
		if api.volumeQueries != 0 {
			t.Fatalf("volume queries = %d, want none when guard is unavailable", api.volumeQueries)
		}
		for name, state := range map[string]FieldState{
			"drive":      target.Probe.OutputVolume.DriveType.State,
			"filesystem": target.Probe.OutputVolume.FileSystem.State,
			"free":       target.Probe.OutputVolume.AvailableBytesCaller.State,
		} {
			if state != Unsupported {
				t.Fatalf("%s state = %s, want UNSUPPORTED", name, state)
			}
		}
	})
	t.Run("restore failure is surfaced", func(t *testing.T) {
		api := newFakeWindowsAPI()
		api.threadErrorModeRestoreErr = &apiError{reason: "RESTORE_THREAD_ERROR_MODE_FAILED", code: 5}
		target := probeWindows(context.Background(), `C:\safe\output`, Options{}, api)
		if api.volumeQueries != 3 || api.volumeQueriesWithoutGuard {
			t.Fatalf("unexpected guarded volume query state: queries=%d unguarded=%v", api.volumeQueries, api.volumeQueriesWithoutGuard)
		}
		if field := target.Probe.OutputVolume.AvailableBytesCaller; field.State != Failed || field.Value != nil || field.ErrorReason != "RESTORE_THREAD_ERROR_MODE_FAILED" {
			t.Fatalf("restore failure was not surfaced: %+v", field)
		}
	})
}

func TestWindowsCPUPressureFailureAndContextCancellation(t *testing.T) {
	t.Parallel()

	t.Run("API failure", func(t *testing.T) {
		api := newFakeWindowsAPI()
		api.systemTimesErrAt = 1
		target := probeWindows(context.Background(), `C:\safe\output`, Options{IncludeCPUPressure: true}, api)
		if target.Probe.CPUBusyBasisPoints == nil || target.Probe.CPUBusyBasisPoints.State != Failed || target.Probe.CPUBusyBasisPoints.Value != nil {
			t.Fatalf("CPU failure = %+v", target.Probe.CPUBusyBasisPoints)
		}
	})

	t.Run("cancelled wait", func(t *testing.T) {
		api := newFakeWindowsAPI()
		api.waitErr = context.Canceled
		target := probeWindows(context.Background(), `C:\safe\output`, Options{IncludeCPUPressure: true}, api)
		if target.Probe.CPUBusyBasisPoints == nil || target.Probe.CPUBusyBasisPoints.State != Failed || target.Probe.CPUBusyBasisPoints.ErrorReason != "CONTEXT_CANCELLED" {
			t.Fatalf("CPU cancellation = %+v", target.Probe.CPUBusyBasisPoints)
		}
	})
}

func TestWindowsOSProbeUnavailableDoesNotInventVersion(t *testing.T) {
	t.Parallel()
	api := newFakeWindowsAPI()
	api.versionErr = errAPIUnavailable
	target := probeWindows(context.Background(), `C:\safe\output`, Options{}, api)
	if target.Version != "" || target.Build != "" || target.Probe.OSVersion.State != Unsupported || target.Probe.OSVersion.Value != nil {
		t.Fatalf("OS version was fabricated: target=%+v field=%+v", target, target.Probe.OSVersion)
	}
	if err := target.Validate(); err == nil {
		t.Fatal("target with unknown OS version unexpectedly validated")
	}
}

func TestWindowsOSProbeCallFailurePreservesError(t *testing.T) {
	t.Parallel()
	api := newFakeWindowsAPI()
	api.versionErr = &apiError{reason: "NTSTATUS_FAILURE", code: 0xc0000001}
	target := probeWindows(context.Background(), `C:\safe\output`, Options{}, api)
	for name, field := range map[string]Field[string]{
		"version": target.Probe.OSVersion,
		"build":   target.Probe.OSBuild,
	} {
		if field.State != Failed || field.Value != nil || field.ErrorReason != "NTSTATUS_FAILURE" || field.ErrorCode != 0xc0000001 {
			t.Fatalf("%s field = %+v", name, field)
		}
	}
	if err := target.Validate(); err == nil {
		t.Fatal("target with failed OS probe unexpectedly validated")
	}
}

func TestProbeRejectsPathOutsidePATHSAFEContract(t *testing.T) {
	t.Parallel()
	if _, err := Probe(context.Background(), `relative\output`, Options{}); err == nil {
		t.Fatal("Probe() unexpectedly accepted a relative output path")
	}
}

type fakeWindowsAPI struct {
	timeValue time.Time

	versionMajor uint32
	versionMinor uint32
	versionBuild uint32
	versionErr   error

	processArch   string
	nativeMachine uint16
	wowErr        error

	info                  systemInfo
	infoErr               error
	nativeSystemInfoCalls int

	activeProcessorValue uint32
	activeProcessorErr   error

	totalMemory     uint64
	availableMemory uint64
	memoryErr       error

	elevated      bool
	elevationType uint32
	tokenErr      error

	threadErrorModeValue      uint32
	threadErrorModeGetErr     error
	threadErrorModeSetErr     error
	threadErrorModeRestoreErr error
	threadErrorModeSets       []uint32
	volumeQueries             int
	volumeQueriesWithoutGuard bool

	driveTypeValue    string
	driveTypeErr      error
	fileSystemValue   string
	fileSystemErr     error
	availableBytes    uint64
	availableBytesErr error

	systemTimeValues [][3]uint64
	systemTimesCalls int
	systemTimesErrAt int
	waitErr          error
	waitDurations    []time.Duration
}

type fakeThreadLifecycle struct {
	lockCalls   int
	unlockCalls int
}

func (f *fakeThreadLifecycle) Lock()   { f.lockCalls++ }
func (f *fakeThreadLifecycle) Unlock() { f.unlockCalls++ }

func newFakeWindowsAPI() *fakeWindowsAPI {
	return &fakeWindowsAPI{
		timeValue:            testTime(),
		versionMajor:         10,
		versionBuild:         26100,
		processArch:          "amd64",
		nativeMachine:        imageFileMachineAMD64,
		info:                 systemInfo{ProcessorArchitecture: processorArchitectureAMD64, NumberOfProcessors: 8},
		activeProcessorValue: 8,
		totalMemory:          16 << 30,
		availableMemory:      8 << 30,
		elevationType:        tokenElevationTypeLimited,
		driveTypeValue:       "FIXED",
		fileSystemValue:      "NTFS",
		availableBytes:       1 << 30,
		systemTimeValues: [][3]uint64{
			{100, 300, 200},
			{150, 500, 300},
		},
		systemTimesErrAt: -1,
	}
}

func (f *fakeWindowsAPI) now() time.Time { return f.timeValue }
func (f *fakeWindowsAPI) wait(_ context.Context, duration time.Duration) error {
	f.waitDurations = append(f.waitDurations, duration)
	return f.waitErr
}
func (f *fakeWindowsAPI) processArchitecture() string { return f.processArch }
func (f *fakeWindowsAPI) rtlGetVersion() (uint32, uint32, uint32, error) {
	return f.versionMajor, f.versionMinor, f.versionBuild, f.versionErr
}
func (f *fakeWindowsAPI) isWow64Process2() (uint16, uint16, error) {
	return imageFileMachineUnknown, f.nativeMachine, f.wowErr
}
func (f *fakeWindowsAPI) nativeSystemInfo() (systemInfo, error) {
	f.nativeSystemInfoCalls++
	return f.info, f.infoErr
}
func (f *fakeWindowsAPI) activeProcessorCount() (uint32, error) {
	return f.activeProcessorValue, f.activeProcessorErr
}
func (f *fakeWindowsAPI) memoryStatus() (uint64, uint64, error) {
	return f.totalMemory, f.availableMemory, f.memoryErr
}
func (f *fakeWindowsAPI) tokenElevation() (bool, uint32, error) {
	return f.elevated, f.elevationType, f.tokenErr
}
func (f *fakeWindowsAPI) threadErrorMode() (uint32, error) {
	return f.threadErrorModeValue, f.threadErrorModeGetErr
}
func (f *fakeWindowsAPI) setThreadErrorMode(mode uint32) error {
	f.threadErrorModeSets = append(f.threadErrorModeSets, mode)
	if len(f.threadErrorModeSets) == 1 && f.threadErrorModeSetErr != nil {
		return f.threadErrorModeSetErr
	}
	if len(f.threadErrorModeSets) == 2 && f.threadErrorModeRestoreErr != nil {
		return f.threadErrorModeRestoreErr
	}
	f.threadErrorModeValue = mode
	return nil
}
func (f *fakeWindowsAPI) guardedVolumeQuery() {
	f.volumeQueries++
	if f.threadErrorModeValue&semFailCriticalErrors == 0 {
		f.volumeQueriesWithoutGuard = true
	}
}
func (f *fakeWindowsAPI) driveType(string) (string, error) {
	f.guardedVolumeQuery()
	return f.driveTypeValue, f.driveTypeErr
}
func (f *fakeWindowsAPI) fileSystem(string) (string, error) {
	f.guardedVolumeQuery()
	return f.fileSystemValue, f.fileSystemErr
}
func (f *fakeWindowsAPI) availableBytesToCaller(string) (uint64, error) {
	f.guardedVolumeQuery()
	return f.availableBytes, f.availableBytesErr
}
func (f *fakeWindowsAPI) systemTimes() (uint64, uint64, uint64, error) {
	call := f.systemTimesCalls
	f.systemTimesCalls++
	if call == f.systemTimesErrAt {
		return 0, 0, 0, &apiError{reason: "API_CALL_FAILED", code: 5}
	}
	if call >= len(f.systemTimeValues) {
		return 0, 0, 0, errors.New("unexpected systemTimes call")
	}
	value := f.systemTimeValues[call]
	return value[0], value[1], value[2], nil
}
