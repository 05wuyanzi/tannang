// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/netip"

	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
)

const (
	WindowsTransportEndpointProviderID = "windows-iphlpapi-transport-endpoints"
	WindowsTransportEndpointMediaType  = "application/x-ndjson"
	WindowsTransportEndpointSchemaID   = "urn:tannang:artifact:windows-transport-endpoint-record-v0"
	WindowsTransportEndpointMaxBuffer  = uint32(64 * 1024 * 1024)
)

const (
	transportAFINET    = uint32(2)
	transportAFINET6   = uint32(23)
	transportTCPAll    = uint32(5)
	transportUDPAll    = uint32(1)
	transportTCPListen = uint32(2)
)

type transportTableKind uint8

const (
	transportTCP4 transportTableKind = iota
	transportTCP6
	transportUDP4
	transportUDP6
)

type transportTableSpec struct {
	kind     transportTableKind
	family   uint32
	table    uint32
	rowSize  uint64
	protocol string
	ipv6     bool
	tcp      bool
}

var transportTableSpecs = []transportTableSpec{
	{kind: transportTCP4, family: transportAFINET, table: transportTCPAll, rowSize: 24, protocol: "TCP", tcp: true},
	{kind: transportTCP6, family: transportAFINET6, table: transportTCPAll, rowSize: 56, protocol: "TCP", ipv6: true, tcp: true},
	{kind: transportUDP4, family: transportAFINET, table: transportUDPAll, rowSize: 12, protocol: "UDP"},
	{kind: transportUDP6, family: transportAFINET6, table: transportUDPAll, rowSize: 28, protocol: "UDP", ipv6: true},
}

type transportEndpointRecord struct {
	Protocol      string  `json:"protocol"`
	AddressFamily string  `json:"address_family"`
	LocalAddress  string  `json:"local_address"`
	LocalPort     uint16  `json:"local_port"`
	LocalScopeID  *uint32 `json:"local_scope_id"`
	RemoteAddress *string `json:"remote_address"`
	RemotePort    *uint16 `json:"remote_port"`
	RemoteScopeID *uint32 `json:"remote_scope_id"`
	TCPState      *uint32 `json:"tcp_state"`
	OwningPID     uint32  `json:"owning_pid"`
}

type transportTableAPI interface {
	availability() (execution.Reason, error)
	getExtendedTcpTable([]byte, *uint32, bool, uint32, uint32) uint32
	getExtendedUdpTable([]byte, *uint32, bool, uint32, uint32) uint32
}

type windowsTransportEndpointRunner struct {
	descriptor         Descriptor
	artifact           ArtifactDescriptor
	api                transportTableAPI
	availabilityReason execution.Reason
	availabilityError  error
}

var _ StreamingRunner = (*windowsTransportEndpointRunner)(nil)
var _ AvailabilityProber = (*windowsTransportEndpointRunner)(nil)

func NewWindowsTransportEndpointRunner() StreamingRunner {
	return newWindowsTransportEndpointRunner(newPlatformTransportTableAPI())
}

func newWindowsTransportEndpointRunner(api transportTableAPI) *windowsTransportEndpointRunner {
	reason := execution.ReasonAPIUnavailable
	var availabilityErr error
	if api != nil {
		reason, availabilityErr = api.availability()
	}
	if !reason.Valid() || (reason == execution.ReasonNone && availabilityErr != nil) {
		reason = execution.ReasonAPIUnavailable
	}
	available := reason == execution.ReasonNone && availabilityErr == nil
	return &windowsTransportEndpointRunner{
		descriptor: Descriptor{
			ID: WindowsTransportEndpointProviderID, Class: FirstPartyNative,
			Capabilities: []string{capability.WindowsTransportEndpointSnapshotID},
			Requirements: Requirements{
				Platforms: []string{"windows"}, OSFamilies: []string{"WindowsNT"}, Architectures: []string{"amd64", "x86"},
				RequiresElevation: false, Available: available, AvailabilityReason: reason,
			},
			SideEffects: []string{
				"Reads local Windows TCP and UDP transport endpoint tables through the native IP Helper API.",
				"Writes serialized observations only to the caller-owned artifact sink.",
			},
			Quality: Quality{Compatibility: execution.Available, Reason: execution.ReasonNone, Fidelity: 5, Disturbance: 1, Completeness: 5, OutputStability: 5, EvidenceValue: 5},
		},
		artifact: ArtifactDescriptor{MediaType: WindowsTransportEndpointMediaType, ContentSchemaID: WindowsTransportEndpointSchemaID},
		api:      api, availabilityReason: reason, availabilityError: availabilityErr,
	}
}

func (r *windowsTransportEndpointRunner) Descriptor() Descriptor {
	if r == nil {
		return Descriptor{}
	}
	d := r.descriptor
	d.Capabilities = append([]string(nil), d.Capabilities...)
	d.Requirements.Platforms = append([]string(nil), d.Requirements.Platforms...)
	d.Requirements.OSFamilies = append([]string(nil), d.Requirements.OSFamilies...)
	d.Requirements.Architectures = append([]string(nil), d.Requirements.Architectures...)
	d.Requirements.RuntimeLanes = append([]string(nil), d.Requirements.RuntimeLanes...)
	d.SideEffects = append([]string(nil), d.SideEffects...)
	return d
}

func (r *windowsTransportEndpointRunner) Artifact() ArtifactDescriptor {
	if r == nil {
		return ArtifactDescriptor{}
	}
	return r.artifact
}

func (r *windowsTransportEndpointRunner) Probe(ctx context.Context, target fingerprint.TargetFingerprint) (execution.Reason, error) {
	if ctx == nil {
		return execution.ReasonProviderError, errors.New("transport probe context is required")
	}
	if err := ctx.Err(); err != nil {
		return execution.ReasonCancelled, err
	}
	if err := target.Validate(); err != nil {
		return execution.ReasonProviderError, err
	}
	if target.Platform != "windows" || target.OSFamily != "WindowsNT" {
		return execution.ReasonUnsupportedOS, nil
	}
	if target.Architecture != "amd64" && target.Architecture != "x86" {
		return execution.ReasonUnsupportedArch, nil
	}
	if r == nil || r.api == nil {
		return execution.ReasonAPIUnavailable, errors.New("Windows IP Helper API is unavailable")
	}
	if r.availabilityReason != execution.ReasonNone {
		return r.availabilityReason, r.availabilityError
	}
	for _, spec := range transportTableSpecs {
		if err := ctx.Err(); err != nil {
			return execution.ReasonCancelled, err
		}
		size := uint32(0)
		status := r.call(spec, nil, &size)
		if status != transportErrorInsufficientBuffer || size < 4 || size > WindowsTransportEndpointMaxBuffer {
			return transportReason(status), fmt.Errorf("transport table probe returned status %d", status)
		}
	}
	return execution.ReasonNone, nil
}

func (r *windowsTransportEndpointRunner) ExecuteTo(ctx context.Context, request capability.Capability, target fingerprint.TargetFingerprint, writer io.Writer) execution.Result {
	fail := func(state execution.State, reason execution.Reason, detail string) execution.Result {
		return execution.Result{State: state, Reason: reason, Detail: detail, SideEffectSummary: "Read-only local Windows IP Helper table calls; incomplete candidate artifact discarded."}
	}
	if err := request.Validate(); err != nil || request.ID != capability.WindowsTransportEndpointSnapshotID || request.AcquisitionSemantics != capability.StateSnapshot {
		return fail(execution.Failed, execution.ReasonProviderError, "Capability does not match WINDOWS_TRANSPORT_ENDPOINT_SNAPSHOT/STATE_SNAPSHOT")
	}
	if err := target.Validate(); err != nil {
		return fail(execution.Failed, execution.ReasonProviderError, "Target Fingerprint is invalid")
	}
	if target.Platform != "windows" || target.OSFamily != "WindowsNT" {
		return fail(execution.Blocked, execution.ReasonUnsupportedOS, "Windows transport endpoints require windows/WindowsNT")
	}
	if target.Architecture != "amd64" && target.Architecture != "x86" {
		return fail(execution.Blocked, execution.ReasonUnsupportedArch, "Windows transport endpoints support only canonical target architectures amd64 and x86")
	}
	if writer == nil {
		return fail(execution.Failed, execution.ReasonProviderError, "candidate artifact writer is required")
	}
	if ctx == nil {
		return fail(execution.Failed, execution.ReasonProviderError, "execution context is required")
	}
	if err := ctx.Err(); err != nil {
		return fail(execution.Skipped, execution.ReasonCancelled, "transport endpoint execution was cancelled before acquisition")
	}
	if r == nil || r.api == nil {
		return fail(execution.Failed, execution.ReasonAPIUnavailable, "Windows IP Helper API is unavailable")
	}
	if r.availabilityReason != execution.ReasonNone {
		return fail(execution.Failed, r.availabilityReason, "Windows IP Helper API is unavailable")
	}
	for _, spec := range transportTableSpecs {
		if err := ctx.Err(); err != nil {
			return fail(execution.Failed, execution.ReasonCancelled, "transport endpoint execution was cancelled during acquisition")
		}
		data, err := r.acquireTable(ctx, spec)
		if err != nil {
			state, reason := execution.Failed, execution.ReasonProviderError
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				reason = execution.ReasonCancelled
			}
			if errors.Is(err, errTransportAccessDenied) {
				state, reason = execution.Blocked, execution.ReasonPrivilegeRequired
			}
			if errors.Is(err, errTransportUnsupported) {
				reason = execution.ReasonAPIUnavailable
			}
			return fail(state, reason, "Windows transport endpoint table acquisition failed")
		}
		if err := writeTableRows(ctx, spec, data, writer); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return fail(execution.Failed, execution.ReasonCancelled, "transport endpoint execution was cancelled while writing")
			}
			return fail(execution.Failed, execution.ReasonProviderError, "write transport endpoint artifact failed")
		}
	}
	return execution.Result{State: execution.Collected, Reason: execution.ReasonNone, Detail: "One bounded Windows TCP/UDP transport endpoint NDJSON snapshot was written to the caller-owned sink.", SideEffectSummary: "Read-only local Windows IP Helper table calls and one caller-owned artifact write."}
}

var (
	errTransportAccessDenied = errors.New("transport access denied")
	errTransportUnsupported  = errors.New("transport API unsupported")
)

const (
	transportErrorSuccess            = uint32(0)
	transportErrorInsufficientBuffer = uint32(122)
	transportErrorAccessDenied       = uint32(5)
	transportErrorNotSupported       = uint32(50)
)

func transportReason(status uint32) execution.Reason {
	switch status {
	case transportErrorAccessDenied:
		return execution.ReasonPrivilegeRequired
	case transportErrorNotSupported:
		return execution.ReasonAPIUnavailable
	default:
		return execution.ReasonProviderError
	}
}

func (r *windowsTransportEndpointRunner) call(spec transportTableSpec, data []byte, size *uint32) uint32 {
	if spec.tcp {
		return r.api.getExtendedTcpTable(data, size, false, spec.family, spec.table)
	}
	return r.api.getExtendedUdpTable(data, size, false, spec.family, spec.table)
}

func (r *windowsTransportEndpointRunner) acquireTable(ctx context.Context, spec transportTableSpec) ([]byte, error) {
	size := uint32(0)
	status := r.call(spec, nil, &size)
	if status == transportErrorAccessDenied {
		return nil, errTransportAccessDenied
	}
	if status == transportErrorNotSupported {
		return nil, errTransportUnsupported
	}
	if status != transportErrorInsufficientBuffer || size < 4 || size > WindowsTransportEndpointMaxBuffer {
		return nil, fmt.Errorf("invalid initial table size status=%d size=%d", status, size)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		buf := make([]byte, int(size))
		callSize := size
		status = r.call(spec, buf, &callSize)
		if status == transportErrorSuccess {
			if callSize > uint32(len(buf)) || callSize < 4 {
				return nil, errors.New("invalid returned table size")
			}
			return buf[:callSize], nil
		}
		if status == transportErrorAccessDenied {
			return nil, errTransportAccessDenied
		}
		if status == transportErrorNotSupported {
			return nil, errTransportUnsupported
		}
		if status != transportErrorInsufficientBuffer || attempt == 1 || callSize <= size || callSize > WindowsTransportEndpointMaxBuffer {
			return nil, fmt.Errorf("table acquisition failed status=%d size=%d", status, callSize)
		}
		size = callSize
	}
	return nil, errors.New("table acquisition retry exhausted")
}

func writeTableRows(ctx context.Context, spec transportTableSpec, data []byte, writer io.Writer) error {
	if len(data) < 4 {
		return errors.New("table header is truncated")
	}
	count := uint64(binary.LittleEndian.Uint32(data[:4]))
	if spec.rowSize == 0 || count > (math.MaxUint64-4)/spec.rowSize {
		return errors.New("table row size arithmetic overflow")
	}
	total := uint64(4) + count*spec.rowSize
	if total < 4 || total > uint64(len(data)) {
		return errors.New("table row bounds are invalid")
	}
	for index := uint64(0); index < count; index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		start := uint64(4) + index*spec.rowSize
		row, err := decodeTransportRow(spec, data[start:start+spec.rowSize])
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(row)
		if err != nil {
			return err
		}
		if err := writeFull(writer, append(encoded, '\n')); err != nil {
			return err
		}
	}
	return nil
}

func decodeTransportRow(spec transportTableSpec, row []byte) (transportEndpointRecord, error) {
	if uint64(len(row)) != spec.rowSize {
		return transportEndpointRecord{}, errors.New("transport row size is invalid")
	}
	record := transportEndpointRecord{Protocol: spec.protocol, AddressFamily: "IPv4", LocalScopeID: nil, RemoteAddress: nil, RemotePort: nil, RemoteScopeID: nil, TCPState: nil}
	if spec.ipv6 {
		record.AddressFamily = "IPv6"
	}
	if spec.ipv6 {
		local := netip.AddrFrom16([16]byte(row[:16]))
		record.LocalAddress = local.String()
		scope := binary.BigEndian.Uint32(row[16:20])
		record.LocalScopeID = &scope
		port := decodePort(row[20:24])
		record.LocalPort = port
		if spec.tcp {
			state := binary.LittleEndian.Uint32(row[48:52])
			record.TCPState = &state
			pid := binary.LittleEndian.Uint32(row[52:56])
			record.OwningPID = pid
			if state != transportTCPListen {
				remote := netip.AddrFrom16([16]byte(row[24:40]))
				remoteText := remote.String()
				record.RemoteAddress = &remoteText
				remotePort := decodePort(row[44:48])
				record.RemotePort = &remotePort
				remoteScope := binary.BigEndian.Uint32(row[40:44])
				record.RemoteScopeID = &remoteScope
			}
		} else {
			record.OwningPID = binary.LittleEndian.Uint32(row[24:28])
		}
		return record, nil
	}
	record.LocalAddress = ipv4FromNetworkDWORD(row[0:4]).String()
	record.LocalPort = decodePort(row[4:8])
	if spec.tcp {
		state := binary.LittleEndian.Uint32(row[0:4])
		record.TCPState = &state
		record.LocalAddress = ipv4FromNetworkDWORD(row[4:8]).String()
		record.LocalPort = decodePort(row[8:12])
		pid := binary.LittleEndian.Uint32(row[20:24])
		record.OwningPID = pid
		if state != transportTCPListen {
			remoteText := ipv4FromNetworkDWORD(row[12:16]).String()
			record.RemoteAddress = &remoteText
			remotePort := decodePort(row[16:20])
			record.RemotePort = &remotePort
		}
	} else {
		record.OwningPID = binary.LittleEndian.Uint32(row[8:12])
	}
	return record, nil
}

func decodePort(value []byte) uint16 {
	return binary.BigEndian.Uint16(value[:2])
}

func ipv4FromNetworkDWORD(value []byte) netip.Addr {
	if len(value) < 4 {
		return netip.Addr{}
	}
	return netip.AddrFrom4([4]byte{value[0], value[1], value[2], value[3]})
}
