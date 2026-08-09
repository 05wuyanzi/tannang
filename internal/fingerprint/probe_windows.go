// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

//go:build windows

package fingerprint

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"github.com/05wuyanzi/tannang/internal/pathsafe"
)

const (
	allProcessorGroups    = 0xffff
	semFailCriticalErrors = 0x0001

	imageFileMachineUnknown = 0x0000
	imageFileMachineI386    = 0x014c
	imageFileMachineARMNT   = 0x01c4
	imageFileMachineIA64    = 0x0200
	imageFileMachineAMD64   = 0x8664
	imageFileMachineARM64   = 0xaa64

	processorArchitectureIntel = 0
	processorArchitectureARM   = 5
	processorArchitectureIA64  = 6
	processorArchitectureAMD64 = 9
	processorArchitectureARM64 = 12

	tokenElevationTypeDefault = 1
	tokenElevationTypeFull    = 2
	tokenElevationTypeLimited = 3

	driveRemovable = 2
	driveFixed     = 3
	driveRemote    = 4
	driveCDROM     = 5
	driveRAMDisk   = 6
)

var errAPIUnavailable = errors.New("Windows API is unavailable")

type windowsProbeAPI interface {
	now() time.Time
	wait(context.Context, time.Duration) error
	processArchitecture() string
	rtlGetVersion() (uint32, uint32, uint32, error)
	isWow64Process2() (uint16, uint16, error)
	nativeSystemInfo() (systemInfo, error)
	activeProcessorCount() (uint32, error)
	memoryStatus() (uint64, uint64, error)
	tokenElevation() (bool, uint32, error)
	threadErrorMode() (uint32, error)
	setThreadErrorMode(uint32) error
	driveType(string) (string, error)
	fileSystem(string) (string, error)
	availableBytesToCaller(string) (uint64, error)
	systemTimes() (uint64, uint64, uint64, error)
}

func platformProbe(ctx context.Context, outputPath string, options Options) (TargetFingerprint, error) {
	if err := pathsafe.ValidateOutputPath(outputPath); err != nil {
		return TargetFingerprint{}, fmt.Errorf("validate fingerprint output path through PATHSAFE: %w", err)
	}
	target := probeWindows(ctx, outputPath, options, realWindowsAPI{})
	if err := pathsafe.ValidateOutputPath(outputPath); err != nil {
		return TargetFingerprint{}, fmt.Errorf("revalidate fingerprint output path through PATHSAFE: %w", err)
	}
	return target, nil
}

func probeWindows(ctx context.Context, outputPath string, options Options, api windowsProbeAPI) TargetFingerprint {
	target := TargetFingerprint{
		Platform: "windows",
		OSFamily: "WindowsNT",
		Probe:    &ProbeFields{},
	}
	p := target.Probe

	major, minor, build, err := api.rtlGetVersion()
	now := api.now()
	switch {
	case err == nil:
		version := fmt.Sprintf("%d.%d", major, minor)
		buildText := strconv.FormatUint(uint64(build), 10)
		target.Version = version
		target.Build = buildText
		p.OSVersion = knownField(version, "RtlGetVersion", now)
		p.OSBuild = knownField(buildText, "RtlGetVersion", now)
	case errors.Is(err, errAPIUnavailable):
		p.OSVersion = unsupportedField[string]("RtlGetVersion", "API_UNAVAILABLE", now)
		p.OSBuild = unsupportedField[string]("RtlGetVersion", "API_UNAVAILABLE", now)
	default:
		p.OSVersion = failedField[string]("RtlGetVersion", err, now)
		p.OSBuild = failedField[string]("RtlGetVersion", err, now)
	}

	processArchitecture := api.processArchitecture()
	now = api.now()
	if processArchitecture == "" {
		p.ProcessArchitecture = failedField[string]("runtime.GOARCH", &apiError{reason: "INVALID_RESULT"}, now)
	} else {
		p.ProcessArchitecture = knownField(processArchitecture, "runtime.GOARCH", now)
	}

	var cachedSystemInfo *systemInfo
	getSystemInfo := func() (systemInfo, error) {
		if cachedSystemInfo != nil {
			return *cachedSystemInfo, nil
		}
		info, infoErr := api.nativeSystemInfo()
		if infoErr == nil {
			cachedSystemInfo = &info
		}
		return info, infoErr
	}

	_, nativeMachine, err := api.isWow64Process2()
	now = api.now()
	if err == nil {
		architecture, mapErr := architectureFromMachine(nativeMachine)
		if mapErr != nil {
			p.NativeArchitecture = failedField[string]("IsWow64Process2", mapErr, now)
		} else {
			target.Architecture = architecture
			p.NativeArchitecture = knownField(architecture, "IsWow64Process2", now)
		}
	} else if errors.Is(err, errAPIUnavailable) {
		info, fallbackErr := getSystemInfo()
		now = api.now()
		if fallbackErr != nil {
			if errors.Is(fallbackErr, errAPIUnavailable) {
				p.NativeArchitecture = unsupportedField[string]("GetNativeSystemInfo", "API_UNAVAILABLE", now)
			} else {
				p.NativeArchitecture = failedField[string]("GetNativeSystemInfo", fallbackErr, now)
			}
		} else {
			architecture, mapErr := architectureFromProcessor(info.ProcessorArchitecture)
			if mapErr != nil {
				p.NativeArchitecture = failedField[string]("GetNativeSystemInfo", mapErr, now)
			} else {
				target.Architecture = architecture
				p.NativeArchitecture = knownField(architecture, "GetNativeSystemInfo", now)
			}
		}
	} else {
		p.NativeArchitecture = failedField[string]("IsWow64Process2", err, now)
	}

	processorCount, err := api.activeProcessorCount()
	now = api.now()
	if err == nil && processorCount > 0 {
		p.LogicalProcessorCount = knownField(processorCount, "GetActiveProcessorCount", now)
	} else if errors.Is(err, errAPIUnavailable) {
		info, fallbackErr := getSystemInfo()
		now = api.now()
		if fallbackErr != nil {
			if errors.Is(fallbackErr, errAPIUnavailable) {
				p.LogicalProcessorCount = unsupportedField[uint32]("GetNativeSystemInfo", "API_UNAVAILABLE", now)
			} else {
				p.LogicalProcessorCount = failedField[uint32]("GetNativeSystemInfo", fallbackErr, now)
			}
		} else if info.NumberOfProcessors == 0 {
			p.LogicalProcessorCount = failedField[uint32]("GetNativeSystemInfo", &apiError{reason: "INVALID_RESULT"}, now)
		} else {
			p.LogicalProcessorCount = knownField(info.NumberOfProcessors, "GetNativeSystemInfo", now)
		}
	} else if err != nil {
		p.LogicalProcessorCount = failedField[uint32]("GetActiveProcessorCount", err, now)
	} else {
		p.LogicalProcessorCount = failedField[uint32]("GetActiveProcessorCount", &apiError{reason: "INVALID_RESULT"}, now)
	}

	totalMemory, availableMemory, err := api.memoryStatus()
	now = api.now()
	if err != nil {
		p.TotalPhysicalMemoryBytes = windowsFailureField[uint64]("GlobalMemoryStatusEx", err, now)
		p.AvailablePhysicalMemoryBytes = windowsFailureField[uint64]("GlobalMemoryStatusEx", err, now)
	} else if totalMemory == 0 || availableMemory > totalMemory {
		invalid := &apiError{reason: "INVALID_RESULT"}
		p.TotalPhysicalMemoryBytes = failedField[uint64]("GlobalMemoryStatusEx", invalid, now)
		p.AvailablePhysicalMemoryBytes = failedField[uint64]("GlobalMemoryStatusEx", invalid, now)
	} else {
		p.TotalPhysicalMemoryBytes = knownField(totalMemory, "GlobalMemoryStatusEx", now)
		p.AvailablePhysicalMemoryBytes = knownField(availableMemory, "GlobalMemoryStatusEx", now)
	}

	elevated, elevationType, err := api.tokenElevation()
	now = api.now()
	if err != nil {
		p.Elevated = windowsFailureField[bool]("GetTokenInformation", err, now)
		p.TokenElevationType = windowsFailureField[string]("GetTokenInformation", err, now)
	} else {
		typeName, typeErr := tokenElevationTypeName(elevationType)
		if typeErr != nil {
			p.Elevated = failedField[bool]("GetTokenInformation", typeErr, now)
			p.TokenElevationType = failedField[string]("GetTokenInformation", typeErr, now)
		} else {
			target.Elevated = elevated
			target.Privilege = privilegeLabel(elevated, elevationType)
			p.Elevated = knownField(elevated, "GetTokenInformation(TokenElevation)", now)
			p.TokenElevationType = knownField(typeName, "GetTokenInformation(TokenElevationType)", now)
		}
	}

	volumeRoot := filepath.VolumeName(outputPath) + `\`
	p.OutputVolume.ValidatedOutputPath = knownField(outputPath, "pathsafe.ValidateOutputPath", api.now())
	p.OutputVolume.VolumeRoot = knownField(volumeRoot, "filepath.VolumeName", api.now())

	driveType, fileSystem, availableBytes, guardErr := probeGuardedVolumeFacts(api, volumeRoot)
	if guardErr != nil {
		now = api.now()
		p.OutputVolume.DriveType = threadErrorModeFailureField[string](guardErr, now)
		p.OutputVolume.FileSystem = threadErrorModeFailureField[string](guardErr, now)
		p.OutputVolume.AvailableBytesCaller = threadErrorModeFailureField[uint64](guardErr, now)
	} else {
		p.OutputVolume.DriveType = driveType
		p.OutputVolume.FileSystem = fileSystem
		p.OutputVolume.AvailableBytesCaller = availableBytes
	}

	if options.IncludeCPUPressure {
		field := sampleCPUPressure(ctx, api)
		p.CPUBusyBasisPoints = &field
	}
	return target
}

func sampleCPUPressure(ctx context.Context, api windowsProbeAPI) Field[uint32] {
	idleBefore, kernelBefore, userBefore, err := api.systemTimes()
	now := api.now()
	if err != nil {
		return windowsFailureField[uint32]("GetSystemTimes", err, now)
	}
	if err := api.wait(ctx, cpuPressureSampleInterval); err != nil {
		return failedField[uint32]("GetSystemTimes", &apiError{reason: "CONTEXT_CANCELLED"}, api.now())
	}
	idleAfter, kernelAfter, userAfter, err := api.systemTimes()
	now = api.now()
	if err != nil {
		return windowsFailureField[uint32]("GetSystemTimes", err, now)
	}
	if idleAfter < idleBefore || kernelAfter < kernelBefore || userAfter < userBefore {
		return failedField[uint32]("GetSystemTimes", &apiError{reason: "NON_MONOTONIC_COUNTER"}, now)
	}
	idleDelta := idleAfter - idleBefore
	kernelDelta := kernelAfter - kernelBefore
	userDelta := userAfter - userBefore
	total := kernelDelta + userDelta
	if total == 0 || idleDelta > kernelDelta {
		return failedField[uint32]("GetSystemTimes", &apiError{reason: "INVALID_RESULT"}, now)
	}
	busy := total - idleDelta
	basisPoints := uint32((busy * 10000) / total)
	return knownField(basisPoints, "GetSystemTimes", now)
}

func windowsFailureField[T any](source string, err error, capturedAt time.Time) Field[T] {
	if errors.Is(err, errAPIUnavailable) {
		return unsupportedField[T](source, "API_UNAVAILABLE", capturedAt)
	}
	return failedField[T](source, err, capturedAt)
}

func threadErrorModeFailureField[T any](err error, capturedAt time.Time) Field[T] {
	if errors.Is(err, errAPIUnavailable) {
		return unsupportedField[T]("ThreadErrorModeGuard", "API_UNAVAILABLE", capturedAt)
	}
	return failedField[T]("ThreadErrorModeGuard", err, capturedAt)
}

// threadLifecycle is deliberately limited to the LockOSThread lifecycle that
// contains Windows thread-local error-mode state. It is injectable only to
// verify that a thread is not unlocked after failed restoration.
type threadLifecycle interface {
	Lock()
	Unlock()
}

type runtimeThreadLifecycle struct{}

func (runtimeThreadLifecycle) Lock()   { runtime.LockOSThread() }
func (runtimeThreadLifecycle) Unlock() { runtime.UnlockOSThread() }

type guardedVolumeResult struct {
	drive          Field[string]
	fileSystem     Field[string]
	availableBytes Field[uint64]
	guardErr       error
}

func probeGuardedVolumeFacts(api windowsProbeAPI, volumeRoot string) (drive Field[string], fileSystem Field[string], availableBytes Field[uint64], guardErr error) {
	return probeGuardedVolumeFactsWithThreadLifecycle(api, volumeRoot, runtimeThreadLifecycle{})
}

func probeGuardedVolumeFactsWithThreadLifecycle(api windowsProbeAPI, volumeRoot string, lifecycle threadLifecycle) (drive Field[string], fileSystem Field[string], availableBytes Field[uint64], guardErr error) {
	results, workerDone := startGuardedVolumeWorker(api, volumeRoot, lifecycle)
	result := <-results
	<-workerDone
	return result.drive, result.fileSystem, result.availableBytes, result.guardErr
}

func startGuardedVolumeWorker(api windowsProbeAPI, volumeRoot string, lifecycle threadLifecycle) (<-chan guardedVolumeResult, <-chan struct{}) {
	// The one-result buffer lets a worker whose thread state cannot be restored
	// publish its failure and exit while still locked, without waiting for its
	// synchronous caller to receive the result.
	results := make(chan guardedVolumeResult, 1)
	workerDone := make(chan struct{})
	go func() {
		results <- probeGuardedVolumeFactsOnLockedThread(api, volumeRoot, lifecycle)
		close(workerDone)
	}()
	return results, workerDone
}

func probeGuardedVolumeFactsOnLockedThread(api windowsProbeAPI, volumeRoot string, lifecycle threadLifecycle) (result guardedVolumeResult) {
	lifecycle.Lock()
	threadStateSafeForReuse := true
	defer func() {
		if threadStateSafeForReuse {
			lifecycle.Unlock()
		}
	}()

	previousMode, err := api.threadErrorMode()
	if err != nil {
		result.guardErr = err
		return result
	}
	if err := api.setThreadErrorMode(previousMode | semFailCriticalErrors); err != nil {
		// A BOOL failure from SetThreadErrorMode establishes that the requested
		// thread-local mode was not installed, so the thread remains reusable.
		result.guardErr = err
		return result
	}
	threadStateSafeForReuse = false
	defer func() {
		if err := api.setThreadErrorMode(previousMode); err != nil {
			result.guardErr = err
			return
		}
		threadStateSafeForReuse = true
	}()

	driveType, err := api.driveType(volumeRoot)
	now := api.now()
	if err != nil {
		result.drive = windowsFailureField[string]("GetDriveTypeW", err, now)
	} else {
		result.drive = knownField(driveType, "GetDriveTypeW", now)
	}

	fileSystemName, err := api.fileSystem(volumeRoot)
	now = api.now()
	if err != nil {
		result.fileSystem = windowsFailureField[string]("GetVolumeInformationW", err, now)
	} else if fileSystemName == "" {
		result.fileSystem = unavailableField[string]("GetVolumeInformationW", "EMPTY_FILESYSTEM_NAME", now)
	} else {
		result.fileSystem = knownField(fileSystemName, "GetVolumeInformationW", now)
	}

	freeBytes, err := api.availableBytesToCaller(volumeRoot)
	now = api.now()
	if err != nil {
		result.availableBytes = windowsFailureField[uint64]("GetDiskFreeSpaceExW", err, now)
	} else {
		result.availableBytes = knownField(freeBytes, "GetDiskFreeSpaceExW", now)
	}
	return result
}

func architectureFromMachine(machine uint16) (string, error) {
	switch machine {
	case imageFileMachineI386:
		return "x86", nil
	case imageFileMachineAMD64:
		return "amd64", nil
	case imageFileMachineARMNT:
		return "arm", nil
	case imageFileMachineARM64:
		return "arm64", nil
	case imageFileMachineIA64:
		return "ia64", nil
	case imageFileMachineUnknown:
		return "", &apiError{reason: "UNKNOWN_MACHINE_TYPE"}
	default:
		return "", &apiError{reason: "UNSUPPORTED_MACHINE_TYPE", code: uint32(machine)}
	}
}

func architectureFromProcessor(architecture uint16) (string, error) {
	switch architecture {
	case processorArchitectureIntel:
		return "x86", nil
	case processorArchitectureAMD64:
		return "amd64", nil
	case processorArchitectureARM:
		return "arm", nil
	case processorArchitectureARM64:
		return "arm64", nil
	case processorArchitectureIA64:
		return "ia64", nil
	default:
		return "", &apiError{reason: "UNSUPPORTED_PROCESSOR_ARCHITECTURE", code: uint32(architecture)}
	}
}

func tokenElevationTypeName(value uint32) (string, error) {
	switch value {
	case tokenElevationTypeDefault:
		return "default", nil
	case tokenElevationTypeFull:
		return "full", nil
	case tokenElevationTypeLimited:
		return "limited", nil
	default:
		return "", &apiError{reason: "UNKNOWN_TOKEN_ELEVATION_TYPE", code: value}
	}
}

func privilegeLabel(elevated bool, elevationType uint32) string {
	if elevated {
		return "elevated"
	}
	if elevationType == tokenElevationTypeLimited {
		return "filtered-admin"
	}
	return "standard-user"
}

type realWindowsAPI struct{}

var (
	ntdll                       = syscall.NewLazyDLL("ntdll.dll")
	kernel32                    = syscall.NewLazyDLL("kernel32.dll")
	procRtlGetVersion           = ntdll.NewProc("RtlGetVersion")
	procIsWow64Process2         = kernel32.NewProc("IsWow64Process2")
	procGetNativeSystemInfo     = kernel32.NewProc("GetNativeSystemInfo")
	procGetActiveProcessorCount = kernel32.NewProc("GetActiveProcessorCount")
	procGlobalMemoryStatusEx    = kernel32.NewProc("GlobalMemoryStatusEx")
	procGetThreadErrorMode      = kernel32.NewProc("GetThreadErrorMode")
	procSetThreadErrorMode      = kernel32.NewProc("SetThreadErrorMode")
	procGetDriveTypeW           = kernel32.NewProc("GetDriveTypeW")
	procGetVolumeInformationW   = kernel32.NewProc("GetVolumeInformationW")
	procGetDiskFreeSpaceExW     = kernel32.NewProc("GetDiskFreeSpaceExW")
	procGetSystemTimes          = kernel32.NewProc("GetSystemTimes")
)

func (realWindowsAPI) now() time.Time { return time.Now().UTC() }

func (realWindowsAPI) wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (realWindowsAPI) processArchitecture() string { return runtime.GOARCH }

type osVersionInfoEx struct {
	Size             uint32
	MajorVersion     uint32
	MinorVersion     uint32
	BuildNumber      uint32
	PlatformID       uint32
	CSDVersion       [128]uint16
	ServicePackMajor uint16
	ServicePackMinor uint16
	SuiteMask        uint16
	ProductType      byte
	Reserved         byte
}

func (realWindowsAPI) rtlGetVersion() (uint32, uint32, uint32, error) {
	if err := procRtlGetVersion.Find(); err != nil {
		return 0, 0, 0, errAPIUnavailable
	}
	info := osVersionInfoEx{Size: uint32(unsafe.Sizeof(osVersionInfoEx{}))}
	status, _, _ := procRtlGetVersion.Call(uintptr(unsafe.Pointer(&info)))
	if status != 0 {
		return 0, 0, 0, &apiError{reason: "NTSTATUS_FAILURE", code: uint32(status)}
	}
	if info.MajorVersion == 0 || info.BuildNumber == 0 {
		return 0, 0, 0, &apiError{reason: "INVALID_RESULT"}
	}
	return info.MajorVersion, info.MinorVersion, info.BuildNumber, nil
}

func (realWindowsAPI) isWow64Process2() (uint16, uint16, error) {
	if err := procIsWow64Process2.Find(); err != nil {
		return 0, 0, errAPIUnavailable
	}
	process, err := syscall.GetCurrentProcess()
	if err != nil {
		return 0, 0, windowsCallError("GET_CURRENT_PROCESS_FAILED", err)
	}
	var processMachine uint16
	var nativeMachine uint16
	result, _, callErr := procIsWow64Process2.Call(
		uintptr(process),
		uintptr(unsafe.Pointer(&processMachine)),
		uintptr(unsafe.Pointer(&nativeMachine)),
	)
	if result == 0 {
		return 0, 0, windowsCallError("API_CALL_FAILED", callErr)
	}
	return processMachine, nativeMachine, nil
}

type systemInfo struct {
	ProcessorArchitecture     uint16
	Reserved                  uint16
	PageSize                  uint32
	MinimumApplicationAddress uintptr
	MaximumApplicationAddress uintptr
	ActiveProcessorMask       uintptr
	NumberOfProcessors        uint32
	ProcessorType             uint32
	AllocationGranularity     uint32
	ProcessorLevel            uint16
	ProcessorRevision         uint16
}

func (realWindowsAPI) nativeSystemInfo() (systemInfo, error) {
	if err := procGetNativeSystemInfo.Find(); err != nil {
		return systemInfo{}, errAPIUnavailable
	}
	var info systemInfo
	procGetNativeSystemInfo.Call(uintptr(unsafe.Pointer(&info)))
	return info, nil
}

func (realWindowsAPI) activeProcessorCount() (uint32, error) {
	if err := procGetActiveProcessorCount.Find(); err != nil {
		return 0, errAPIUnavailable
	}
	result, _, callErr := procGetActiveProcessorCount.Call(allProcessorGroups)
	if result == 0 {
		return 0, windowsCallError("API_CALL_FAILED", callErr)
	}
	return uint32(result), nil
}

type memoryStatusEx struct {
	Length            uint32
	MemoryLoad        uint32
	TotalPhysical     uint64
	AvailablePhysical uint64
	TotalPageFile     uint64
	AvailablePageFile uint64
	TotalVirtual      uint64
	AvailableVirtual  uint64
	AvailableExtended uint64
}

func (realWindowsAPI) memoryStatus() (uint64, uint64, error) {
	if err := procGlobalMemoryStatusEx.Find(); err != nil {
		return 0, 0, errAPIUnavailable
	}
	status := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	result, _, callErr := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if result == 0 {
		return 0, 0, windowsCallError("API_CALL_FAILED", callErr)
	}
	return status.TotalPhysical, status.AvailablePhysical, nil
}

func (realWindowsAPI) tokenElevation() (bool, uint32, error) {
	process, err := syscall.GetCurrentProcess()
	if err != nil {
		return false, 0, windowsCallError("GET_CURRENT_PROCESS_FAILED", err)
	}
	var token syscall.Token
	if err := syscall.OpenProcessToken(process, syscall.TOKEN_QUERY, &token); err != nil {
		return false, 0, windowsCallError("OPEN_PROCESS_TOKEN_FAILED", err)
	}
	defer token.Close()

	var elevation uint32
	var returned uint32
	if err := syscall.GetTokenInformation(
		token,
		syscall.TokenElevation,
		(*byte)(unsafe.Pointer(&elevation)),
		uint32(unsafe.Sizeof(elevation)),
		&returned,
	); err != nil {
		return false, 0, windowsCallError("TOKEN_ELEVATION_QUERY_FAILED", err)
	}
	var elevationType uint32
	if err := syscall.GetTokenInformation(
		token,
		syscall.TokenElevationType,
		(*byte)(unsafe.Pointer(&elevationType)),
		uint32(unsafe.Sizeof(elevationType)),
		&returned,
	); err != nil {
		return false, 0, windowsCallError("TOKEN_ELEVATION_TYPE_QUERY_FAILED", err)
	}
	return elevation != 0, elevationType, nil
}

func (realWindowsAPI) threadErrorMode() (uint32, error) {
	if err := procGetThreadErrorMode.Find(); err != nil {
		return 0, errAPIUnavailable
	}
	result, _, callErr := procGetThreadErrorMode.Call()
	return threadErrorModeFromRawResult(result, callErr), nil
}

func (realWindowsAPI) setThreadErrorMode(mode uint32) error {
	if err := procSetThreadErrorMode.Find(); err != nil {
		return errAPIUnavailable
	}
	var previousMode uint32
	result, _, callErr := procSetThreadErrorMode.Call(
		uintptr(mode),
		uintptr(unsafe.Pointer(&previousMode)),
	)
	return setThreadErrorModeFromRawResult(result, callErr)
}

// threadErrorModeFromRawResult follows GetThreadErrorMode's DWORD return
// contract. Zero is the valid system/default error mode; raw Proc.Call
// last-error state is not a failure signal for this API.
func threadErrorModeFromRawResult(result uintptr, _ error) uint32 {
	return uint32(result)
}

// setThreadErrorModeFromRawResult follows SetThreadErrorMode's BOOL return
// contract. The raw last-error value is meaningful only when BOOL is zero.
func setThreadErrorModeFromRawResult(result uintptr, callErr error) error {
	if result == 0 {
		return windowsCallError("SET_THREAD_ERROR_MODE_FAILED", callErr)
	}
	return nil
}

func (realWindowsAPI) driveType(root string) (string, error) {
	if err := procGetDriveTypeW.Find(); err != nil {
		return "", errAPIUnavailable
	}
	pointer, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return "", &apiError{reason: "INVALID_PATH_ENCODING"}
	}
	result, _, callErr := procGetDriveTypeW.Call(uintptr(unsafe.Pointer(pointer)))
	switch uint32(result) {
	case driveRemovable:
		return "REMOVABLE", nil
	case driveFixed:
		return "FIXED", nil
	case driveRemote:
		return "REMOTE", nil
	case driveCDROM:
		return "CDROM", nil
	case driveRAMDisk:
		return "RAMDISK", nil
	case 0:
		return "", windowsCallError("DRIVE_TYPE_UNKNOWN", callErr)
	case 1:
		return "", &apiError{reason: "DRIVE_ROOT_UNAVAILABLE"}
	default:
		return "", &apiError{reason: "UNKNOWN_DRIVE_TYPE", code: uint32(result)}
	}
}

func (realWindowsAPI) fileSystem(root string) (string, error) {
	if err := procGetVolumeInformationW.Find(); err != nil {
		return "", errAPIUnavailable
	}
	pointer, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return "", &apiError{reason: "INVALID_PATH_ENCODING"}
	}
	fileSystem := make([]uint16, 64)
	result, _, callErr := procGetVolumeInformationW.Call(
		uintptr(unsafe.Pointer(pointer)),
		0,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&fileSystem[0])),
		uintptr(len(fileSystem)),
	)
	if result == 0 {
		return "", windowsCallError("API_CALL_FAILED", callErr)
	}
	return syscall.UTF16ToString(fileSystem), nil
}

func (realWindowsAPI) availableBytesToCaller(root string) (uint64, error) {
	if err := procGetDiskFreeSpaceExW.Find(); err != nil {
		return 0, errAPIUnavailable
	}
	pointer, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return 0, &apiError{reason: "INVALID_PATH_ENCODING"}
	}
	var available uint64
	result, _, callErr := procGetDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(pointer)),
		uintptr(unsafe.Pointer(&available)),
		0,
		0,
	)
	if result == 0 {
		return 0, windowsCallError("API_CALL_FAILED", callErr)
	}
	return available, nil
}

type fileTime struct {
	LowDateTime  uint32
	HighDateTime uint32
}

func (t fileTime) uint64() uint64 {
	return uint64(t.HighDateTime)<<32 | uint64(t.LowDateTime)
}

func (realWindowsAPI) systemTimes() (uint64, uint64, uint64, error) {
	if err := procGetSystemTimes.Find(); err != nil {
		return 0, 0, 0, errAPIUnavailable
	}
	var idle fileTime
	var kernel fileTime
	var user fileTime
	result, _, callErr := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if result == 0 {
		return 0, 0, 0, windowsCallError("API_CALL_FAILED", callErr)
	}
	return idle.uint64(), kernel.uint64(), user.uint64(), nil
}

func windowsCallError(reason string, err error) error {
	var errno syscall.Errno
	if errors.As(err, &errno) && errno != 0 {
		return &apiError{reason: reason, code: uint32(errno)}
	}
	return &apiError{reason: reason}
}
