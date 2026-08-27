// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package provider

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/05wuyanzi/tannang/internal/capability"
	"github.com/05wuyanzi/tannang/internal/execution"
	"github.com/05wuyanzi/tannang/internal/fingerprint"
)

type fakeTransportTableAPI struct {
	availabilityReason execution.Reason
	availabilityErr    error
	tables             map[transportTableKind][]byte
	probeCalls         []transportTableKind
	dataCalls          []transportTableKind
	orderArgs          []bool
	probeSuccess       bool
	probeSizes         map[transportTableKind]uint32
	growOnce           map[transportTableKind]bool
	growAlways         map[transportTableKind]bool
	deny               map[transportTableKind]bool
}

func (f *fakeTransportTableAPI) availability() (execution.Reason, error) {
	return f.availabilityReason, f.availabilityErr
}

func (f *fakeTransportTableAPI) getExtendedTcpTable(buffer []byte, size *uint32, order bool, family, table uint32) uint32 {
	f.orderArgs = append(f.orderArgs, order)
	return f.get(transportTableKindFor(family, table, true), buffer, size)
}

func (f *fakeTransportTableAPI) getExtendedUdpTable(buffer []byte, size *uint32, order bool, family, table uint32) uint32 {
	f.orderArgs = append(f.orderArgs, order)
	return f.get(transportTableKindFor(family, table, false), buffer, size)
}

func transportTableKindFor(family, table uint32, tcp bool) transportTableKind {
	if tcp && family == transportAFINET && table == transportTCPAll {
		return transportTCP4
	}
	if tcp && family == transportAFINET6 && table == transportTCPAll {
		return transportTCP6
	}
	if !tcp && family == transportAFINET && table == transportUDPAll {
		return transportUDP4
	}
	return transportUDP6
}

func (f *fakeTransportTableAPI) get(kind transportTableKind, buffer []byte, size *uint32) uint32 {
	if size == nil {
		return transportErrorProvider
	}
	if buffer == nil {
		f.probeCalls = append(f.probeCalls, kind)
		if f.deny[kind] {
			return transportErrorAccessDenied
		}
		*size = uint32(len(f.tables[kind]))
		if override, ok := f.probeSizes[kind]; ok {
			*size = override
		}
		if f.probeSuccess {
			return transportErrorSuccess
		}
		return transportErrorInsufficientBuffer
	}
	f.dataCalls = append(f.dataCalls, kind)
	if f.deny[kind] {
		return transportErrorAccessDenied
	}
	if f.growOnce[kind] {
		delete(f.growOnce, kind)
		*size = uint32(len(f.tables[kind]) + 4)
		return transportErrorInsufficientBuffer
	}
	if f.growAlways[kind] {
		*size = uint32(len(f.tables[kind]) + 4)
		return transportErrorInsufficientBuffer
	}
	if uint32(len(f.tables[kind])) > *size {
		*size = uint32(len(f.tables[kind]))
		return transportErrorInsufficientBuffer
	}
	copy(buffer, f.tables[kind])
	*size = uint32(len(f.tables[kind]))
	return transportErrorSuccess
}

const transportErrorProvider = uint32(87)

func transportTestTarget() fingerprint.TargetFingerprint {
	return fingerprint.TargetFingerprint{Platform: "windows", OSFamily: "WindowsNT", Version: "10.0", Build: "1", Architecture: "amd64", Privilege: "standard-user", RuntimeLane: "portable"}
}

func newTransportTestAPI() *fakeTransportTableAPI {
	return &fakeTransportTableAPI{
		availabilityReason: execution.ReasonNone,
		tables: map[transportTableKind][]byte{
			transportTCP4: transportTable(24, tcp4Rows()),
			transportTCP6: transportTable(56, tcp6Rows()),
			transportUDP4: transportTable(12, udp4Rows()),
			transportUDP6: transportTable(28, udp6Rows()),
		},
		growOnce: map[transportTableKind]bool{}, growAlways: map[transportTableKind]bool{}, deny: map[transportTableKind]bool{}, probeSizes: map[transportTableKind]uint32{},
	}
}

func transportTable(rowSize int, rows ...[]byte) []byte {
	data := make([]byte, 4+rowSize*len(rows))
	binary.LittleEndian.PutUint32(data[:4], uint32(len(rows)))
	for index, row := range rows {
		copy(data[4+index*rowSize:], row)
	}
	return data
}

func tcp4Rows() []byte {
	row := make([]byte, 24)
	binary.LittleEndian.PutUint32(row[0:4], 5)
	copy(row[4:8], []byte{1, 2, 3, 4})
	copy(row[8:12], []byte{0x12, 0x34, 0, 0})
	copy(row[12:16], []byte{5, 6, 7, 8})
	copy(row[16:20], []byte{0, 0x50, 0, 0})
	binary.LittleEndian.PutUint32(row[20:24], 1234)
	return row
}

func tcp6Rows() []byte {
	row := make([]byte, 56)
	copy(row[0:16], []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1})
	binary.BigEndian.PutUint32(row[16:20], 0x01020304)
	copy(row[20:24], []byte{0xab, 0xcd, 0, 0})
	copy(row[24:40], []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2})
	binary.BigEndian.PutUint32(row[40:44], 0x05060708)
	copy(row[44:48], []byte{0x01, 0xbb, 0, 0})
	binary.LittleEndian.PutUint32(row[48:52], 8)
	binary.LittleEndian.PutUint32(row[52:56], 4321)
	return row
}

func udp4Rows() []byte {
	row := make([]byte, 12)
	copy(row[0:4], []byte{10, 20, 30, 40})
	copy(row[4:8], []byte{0x13, 0x88, 0, 0})
	binary.LittleEndian.PutUint32(row[8:12], 0)
	return row
}

func udp6Rows() []byte {
	row := make([]byte, 28)
	copy(row[0:16], []byte{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0x02, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 1})
	binary.BigEndian.PutUint32(row[16:20], 9)
	copy(row[20:24], []byte{0x1f, 0x90, 0, 0})
	binary.LittleEndian.PutUint32(row[24:28], 55)
	return row
}

func TestWindowsTransportEndpointDescriptorAndArtifact(t *testing.T) {
	runner := newWindowsTransportEndpointRunner(newTransportTestAPI())
	d := runner.Descriptor()
	if d.ID != WindowsTransportEndpointProviderID || d.Class != FirstPartyNative || !d.Supports(capability.WindowsTransportEndpointSnapshotID) || !d.Requirements.Available {
		t.Fatalf("descriptor=%+v", d)
	}
	if artifact := runner.Artifact(); artifact.MediaType != WindowsTransportEndpointMediaType || artifact.ContentSchemaID != WindowsTransportEndpointSchemaID {
		t.Fatalf("artifact=%+v", artifact)
	}
}

func TestWindowsTransportEndpointProbeUsesFourNilBufferCalls(t *testing.T) {
	api := newTransportTestAPI()
	runner := newWindowsTransportEndpointRunner(api)
	reason, err := runner.Probe(context.Background(), transportTestTarget())
	if err != nil || reason != execution.ReasonNone {
		t.Fatalf("reason=%s err=%v", reason, err)
	}
	if len(api.probeCalls) != 4 {
		t.Fatalf("probe calls=%v", api.probeCalls)
	}
	if len(api.dataCalls) != 0 {
		t.Fatalf("probe unexpectedly acquired table rows: %v", api.dataCalls)
	}
	for _, order := range api.orderArgs {
		if order {
			t.Fatal("native API was asked to sort rows")
		}
	}
	for index, kind := range []transportTableKind{transportTCP4, transportTCP6, transportUDP4, transportUDP6} {
		if api.probeCalls[index] != kind {
			t.Fatalf("probe order=%v", api.probeCalls)
		}
	}
}

func TestWindowsTransportEndpointRunnerSerializesAllFamilies(t *testing.T) {
	runner := newWindowsTransportEndpointRunner(newTransportTestAPI())
	var out bytes.Buffer
	result := runner.ExecuteTo(context.Background(), capability.WindowsTransportEndpointSnapshot(), transportTestTarget(), &out)
	if result.State != execution.Collected || result.Reason != execution.ReasonNone {
		t.Fatalf("result=%+v", result)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("lines=%d output=%q", len(lines), out.String())
	}
	checks := []string{
		`{"protocol":"TCP","address_family":"IPv4","local_address":"1.2.3.4","local_port":4660,"local_scope_id":null,"remote_address":"5.6.7.8","remote_port":80,"remote_scope_id":null,"tcp_state":5,"owning_pid":1234}`,
		`{"protocol":"TCP","address_family":"IPv6","local_address":"2001:db8::1","local_port":43981,"local_scope_id":16909060,"remote_address":"2001:db8::2","remote_port":443,"remote_scope_id":84281096,"tcp_state":8,"owning_pid":4321}`,
		`{"protocol":"UDP","address_family":"IPv4","local_address":"10.20.30.40","local_port":5000,"local_scope_id":null,"remote_address":null,"remote_port":null,"remote_scope_id":null,"tcp_state":null,"owning_pid":0}`,
		`{"protocol":"UDP","address_family":"IPv6","local_address":"fe80::2aa:bbcc:ddee:ff01","local_port":8080,"local_scope_id":9,"remote_address":null,"remote_port":null,"remote_scope_id":null,"tcp_state":null,"owning_pid":55}`,
	}
	for index, want := range checks {
		if lines[index] != want {
			t.Errorf("line %d=%q want %q", index, lines[index], want)
		}
	}
}

func TestWindowsTransportEndpointListenAndUDPNullSemantics(t *testing.T) {
	api := newTransportTestAPI()
	listen := make([]byte, 24)
	binary.LittleEndian.PutUint32(listen[0:4], transportTCPListen)
	copy(listen[4:8], []byte{127, 0, 0, 1})
	copy(listen[8:12], []byte{0x00, 0x50, 0, 0})
	copy(listen[12:16], []byte{8, 8, 8, 8})
	copy(listen[16:20], []byte{0x01, 0xbb, 0, 0})
	binary.LittleEndian.PutUint32(listen[20:24], 7)
	api.tables[transportTCP4] = transportTable(24, listen)
	var out bytes.Buffer
	result := newWindowsTransportEndpointRunner(api).ExecuteTo(context.Background(), capability.WindowsTransportEndpointSnapshot(), transportTestTarget(), &out)
	if result.State != execution.Collected {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(out.String(), `"remote_address":null,"remote_port":null,"remote_scope_id":null`) {
		t.Fatalf("listen remote fields were not null: %s", out.String())
	}
}

func TestWindowsTransportEndpointGrowthRetryAndExhaustion(t *testing.T) {
	api := newTransportTestAPI()
	api.growOnce[transportTCP4] = true
	var out bytes.Buffer
	result := newWindowsTransportEndpointRunner(api).ExecuteTo(context.Background(), capability.WindowsTransportEndpointSnapshot(), transportTestTarget(), &out)
	if result.State != execution.Collected || len(api.dataCalls) != 5 {
		t.Fatalf("retry result=%+v calls=%v", result, api.dataCalls)
	}
	api = newTransportTestAPI()
	api.growAlways[transportTCP4] = true
	result = newWindowsTransportEndpointRunner(api).ExecuteTo(context.Background(), capability.WindowsTransportEndpointSnapshot(), transportTestTarget(), io.Discard)
	if result.State != execution.Failed || len(api.dataCalls) != 2 {
		t.Fatalf("unexpected exhaustion result=%+v", result)
	}
}

func TestWindowsTransportEndpointEmptyTablesAreCollected(t *testing.T) {
	api := newTransportTestAPI()
	for kind, spec := range map[transportTableKind]int{transportTCP4: 24, transportTCP6: 56, transportUDP4: 12, transportUDP6: 28} {
		api.tables[kind] = transportTable(spec)
	}
	var out bytes.Buffer
	result := newWindowsTransportEndpointRunner(api).ExecuteTo(context.Background(), capability.WindowsTransportEndpointSnapshot(), transportTestTarget(), &out)
	if result.State != execution.Collected || out.Len() != 0 {
		t.Fatalf("empty result=%+v output=%q", result, out.String())
	}
}

func TestWindowsTransportEndpointUDPIPv6PIDZeroAndOrderPreserved(t *testing.T) {
	api := newTransportTestAPI()
	first, second := udp6Rows(), udp6Rows()
	binary.LittleEndian.PutUint32(first[24:28], 0)
	binary.LittleEndian.PutUint32(second[24:28], 99)
	api.tables[transportUDP6] = transportTable(28, first, second)
	var out bytes.Buffer
	result := newWindowsTransportEndpointRunner(api).ExecuteTo(context.Background(), capability.WindowsTransportEndpointSnapshot(), transportTestTarget(), &out)
	if result.State != execution.Collected {
		t.Fatalf("result=%+v", result)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 5 || !strings.Contains(lines[3], `"owning_pid":0`) || !strings.Contains(lines[4], `"owning_pid":99`) {
		t.Fatalf("rows/order=%q", lines)
	}
}

func TestWindowsTransportEndpointRejectsCountOverflow(t *testing.T) {
	api := newTransportTestAPI()
	bad := make([]byte, 4)
	binary.LittleEndian.PutUint32(bad, ^uint32(0))
	api.tables[transportTCP4] = bad
	result := newWindowsTransportEndpointRunner(api).ExecuteTo(context.Background(), capability.WindowsTransportEndpointSnapshot(), transportTestTarget(), io.Discard)
	if result.State != execution.Failed || result.Reason != execution.ReasonProviderError {
		t.Fatalf("result=%+v", result)
	}
}

func TestWindowsTransportEndpointCancellationAndAccessDenied(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := newWindowsTransportEndpointRunner(newTransportTestAPI()).ExecuteTo(ctx, capability.WindowsTransportEndpointSnapshot(), transportTestTarget(), io.Discard)
	if result.State != execution.Skipped || result.Reason != execution.ReasonCancelled {
		t.Fatalf("before cancellation=%+v", result)
	}
	api := newTransportTestAPI()
	api.deny[transportTCP4] = true
	result = newWindowsTransportEndpointRunner(api).ExecuteTo(context.Background(), capability.WindowsTransportEndpointSnapshot(), transportTestTarget(), io.Discard)
	if result.State != execution.Blocked || result.Reason != execution.ReasonPrivilegeRequired {
		t.Fatalf("access denied=%+v", result)
	}
}

func TestWindowsTransportEndpointCancellationDuringSnapshotIsFailed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	writer := cancelAfterFirstTransportWrite{cancel: cancel}
	result := newWindowsTransportEndpointRunner(newTransportTestAPI()).ExecuteTo(ctx, capability.WindowsTransportEndpointSnapshot(), transportTestTarget(), &writer)
	if result.State != execution.Failed || result.Reason != execution.ReasonCancelled {
		t.Fatalf("mid-snapshot cancellation=%+v", result)
	}
}

func TestWindowsTransportEndpointUnexpectedProbeSuccessIsRejected(t *testing.T) {
	api := newTransportTestAPI()
	api.probeSuccess = true
	reason, err := newWindowsTransportEndpointRunner(api).Probe(context.Background(), transportTestTarget())
	if reason != execution.ReasonProviderError || err == nil {
		t.Fatalf("unexpected nil-buffer success reason=%s err=%v", reason, err)
	}
}

func TestWindowsTransportEndpointRejectsOversizedNativeBufferBeforeAllocation(t *testing.T) {
	api := newTransportTestAPI()
	api.probeSizes[transportTCP4] = WindowsTransportEndpointMaxBuffer + 1
	result := newWindowsTransportEndpointRunner(api).ExecuteTo(context.Background(), capability.WindowsTransportEndpointSnapshot(), transportTestTarget(), io.Discard)
	if result.State != execution.Failed || result.Reason != execution.ReasonProviderError {
		t.Fatalf("oversized table result=%+v", result)
	}
}

func TestWindowsTransportEndpointRejectsMalformedTable(t *testing.T) {
	api := newTransportTestAPI()
	bad := make([]byte, 4)
	binary.LittleEndian.PutUint32(bad, 1)
	api.tables[transportTCP4] = bad
	result := newWindowsTransportEndpointRunner(api).ExecuteTo(context.Background(), capability.WindowsTransportEndpointSnapshot(), transportTestTarget(), io.Discard)
	if result.State != execution.Failed || result.Reason != execution.ReasonProviderError {
		t.Fatalf("result=%+v", result)
	}
}

func TestWindowsTransportEndpointWriterFailureIsNotRetainable(t *testing.T) {
	api := newTransportTestAPI()
	result := newWindowsTransportEndpointRunner(api).ExecuteTo(context.Background(), capability.WindowsTransportEndpointSnapshot(), transportTestTarget(), failingTransportWriter{})
	if result.State != execution.Failed || result.Reason != execution.ReasonProviderError {
		t.Fatalf("result=%+v", result)
	}
}

func TestWindowsTransportEndpointWriterFailureAfterBytesIsNotRetainable(t *testing.T) {
	api := newTransportTestAPI()
	result := newWindowsTransportEndpointRunner(api).ExecuteTo(context.Background(), capability.WindowsTransportEndpointSnapshot(), transportTestTarget(), &partialFailTransportWriter{})
	if result.State != execution.Failed || result.Reason != execution.ReasonProviderError {
		t.Fatalf("result=%+v", result)
	}
}

type failingTransportWriter struct{}

func (failingTransportWriter) Write([]byte) (int, error) { return 0, errors.New("writer failure") }

type partialFailTransportWriter struct{ wrote bool }

func (w *partialFailTransportWriter) Write(data []byte) (int, error) {
	if !w.wrote && len(data) > 0 {
		w.wrote = true
		return len(data) / 2, errors.New("writer failed after partial bytes")
	}
	return 0, errors.New("writer failure")
}

type cancelAfterFirstTransportWrite struct {
	cancel context.CancelFunc
}

func (w *cancelAfterFirstTransportWrite) Write(data []byte) (int, error) {
	w.cancel()
	return len(data), nil
}
