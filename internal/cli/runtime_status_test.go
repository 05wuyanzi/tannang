// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/05wuyanzi/tannang/internal/application"
	"github.com/05wuyanzi/tannang/internal/execution"
)

type runtimeEmittingStage struct {
	sink   application.RuntimeEventSink
	result application.RunResult
}

func (s *runtimeEmittingStage) Run(context.Context, application.RunRequest) (application.RunResult, error) {
	s.sink.TryEmit(application.RuntimeEvent{Type: application.RuntimeTypeStart, Event: application.RuntimeEventRunStarted})
	s.sink.TryEmit(application.RuntimeEvent{Type: application.RuntimeTypeTerminal, Event: application.RuntimeEventRunReturned})
	return s.result, nil
}

type errorRuntimeStage struct {
	sink    application.RuntimeEventSink
	entered <-chan struct{}
}

func (s *errorRuntimeStage) Run(context.Context, application.RunRequest) (application.RunResult, error) {
	s.sink.TryEmit(application.RuntimeEvent{Type: application.RuntimeTypeStart, Event: application.RuntimeEventRunStarted})
	select {
	case <-s.entered:
	case <-time.After(time.Second):
		return application.RunResult{}, errors.New("blocking writer was not reached")
	}
	return application.RunResult{}, errors.New("injected terminal collection failure")
}

type synchronizedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *synchronizedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(value)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

type blockingRuntimeWriter struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *blockingRuntimeWriter) Write(value []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(value), nil
}

type concurrencyDetectWriter struct {
	active atomic.Int32
	max    atomic.Int32
	bytes  atomic.Int64
}

func (w *concurrencyDetectWriter) Write(value []byte) (int, error) {
	active := w.active.Add(1)
	for {
		current := w.max.Load()
		if active <= current || w.max.CompareAndSwap(current, active) {
			break
		}
	}
	time.Sleep(time.Millisecond)
	w.bytes.Add(int64(len(value)))
	w.active.Add(-1)
	return len(value), nil
}

func TestRuntimeReporterWritesOneBoundedStructuredLine(t *testing.T) {
	var output bytes.Buffer
	fixed := time.Date(2026, 8, 21, 1, 2, 3, 456789000, time.UTC)
	reporter := newRuntimeReporter(&output, time.Hour, func() time.Time { return fixed })
	if !reporter.TryEmit(application.RuntimeEvent{Type: application.RuntimeTypePhase, Event: application.RuntimeEventFingerprint}) {
		t.Fatal("runtime event was unexpectedly dropped")
	}
	reporter.Stop()
	line := output.Bytes()
	if len(line) == 0 || len(line) > maxRuntimeRecordBytes || line[len(line)-1] != '\n' || !bytes.HasPrefix(line, []byte(runtimeStatusPrefix)) {
		t.Fatalf("invalid bounded runtime line: %q", line)
	}
	var record runtimeWireRecord
	if err := json.Unmarshal(bytes.TrimSuffix(line[len(runtimeStatusPrefix):], []byte("\n")), &record); err != nil {
		t.Fatalf("decode runtime record: %v", err)
	}
	if record.Type != application.RuntimeTypePhase || record.Event != application.RuntimeEventFingerprint || record.At != fixed.Format(time.RFC3339Nano) {
		t.Fatalf("runtime record=%+v", record)
	}
}

func TestRuntimeReporterHeartbeatAndInvalidCadenceAreSafe(t *testing.T) {
	output := &synchronizedBuffer{}
	reporter := newRuntimeReporter(output, 5*time.Millisecond, time.Now)
	time.Sleep(15 * time.Millisecond)
	if strings.Contains(output.String(), `"type":"HEARTBEAT"`) {
		t.Fatalf("heartbeat preceded RUN_STARTED: %q", output.String())
	}
	if !reporter.TryEmit(application.RuntimeEvent{Type: application.RuntimeTypeStart, Event: application.RuntimeEventRunStarted}) {
		t.Fatal("RUN_STARTED was unexpectedly dropped")
	}
	deadline := time.Now().Add(250 * time.Millisecond)
	for !strings.Contains(output.String(), `"type":"HEARTBEAT"`) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	reporter.Stop()
	if !strings.Contains(output.String(), `"event":"PULSE"`) {
		t.Fatalf("heartbeat not observed: %q", output.String())
	}

	zeroCadence := newRuntimeReporter(io.Discard, 0, nil)
	zeroCadence.Stop()
}

func TestRuntimeReporterDropsWithoutBlockingWhenFull(t *testing.T) {
	writer := &blockingRuntimeWriter{entered: make(chan struct{}), release: make(chan struct{})}
	reporter := newRuntimeReporter(writer, time.Hour, time.Now)
	event := application.RuntimeEvent{Type: application.RuntimeTypeStart, Event: application.RuntimeEventRunStarted}
	if !reporter.TryEmit(event) {
		t.Fatal("initial runtime event was dropped")
	}
	select {
	case <-writer.entered:
	case <-time.After(time.Second):
		t.Fatal("runtime writer did not start")
	}
	for index := 0; index < runtimeEventBuffer; index++ {
		if !reporter.TryEmit(event) {
			t.Fatalf("buffer filled early at event %d", index)
		}
	}
	result := make(chan bool, 1)
	go func() { result <- reporter.TryEmit(event) }()
	select {
	case accepted := <-result:
		if accepted {
			t.Fatal("overflow event was not dropped")
		}
	case <-time.After(time.Second):
		t.Fatal("overflow TryEmit blocked")
	}
	close(writer.release)
	reporter.Stop()
	if reporter.dropped.Load() == 0 {
		t.Fatal("drop accounting was not incremented")
	}
}

func TestRuntimeReporterWriterFailureAndPanicAreIsolated(t *testing.T) {
	for name, writer := range map[string]io.Writer{
		"error": failingTerminalWriterPointer(0, errors.New("injected")),
		"panic": panicWriter{},
	} {
		t.Run(name, func(t *testing.T) {
			reporter := newRuntimeReporter(writer, time.Hour, time.Now)
			reporter.TryEmit(application.RuntimeEvent{Type: application.RuntimeTypeStart, Event: application.RuntimeEventRunStarted})
			deadline := time.Now().Add(time.Second)
			for !reporter.disabled.Load() && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			reporter.Stop()
			if !reporter.disabled.Load() {
				t.Fatal("runtime reporter did not disable after writer failure")
			}
		})
	}
}

func TestStderrArbiterUsesOneUnderlyingWriter(t *testing.T) {
	underlying := &concurrencyDetectWriter{}
	arbiter := newStderrArbiter(underlying)
	var group sync.WaitGroup
	for index := 0; index < 32; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			_, _ = arbiter.Write([]byte("one complete line\n"))
		}()
	}
	group.Wait()
	arbiter.Stop()
	if underlying.max.Load() != 1 || underlying.bytes.Load() != int64(32*len("one complete line\n")) {
		t.Fatalf("max concurrent writes=%d bytes=%d", underlying.max.Load(), underlying.bytes.Load())
	}
}

func TestStderrArbiterQueueFullIsNonblocking(t *testing.T) {
	writer := &blockingRuntimeWriter{entered: make(chan struct{}), release: make(chan struct{})}
	arbiter := newStderrArbiter(writer)
	defer close(writer.release)
	if _, err := arbiter.Write([]byte("first\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-writer.entered:
	case <-time.After(time.Second):
		t.Fatal("arbiter writer did not start")
	}
	for index := 0; index < runtimeEventBuffer; index++ {
		_, _ = arbiter.Write([]byte("queued\n"))
	}
	start := time.Now()
	_, _ = arbiter.Write([]byte("overflow\n"))
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("arbiter Write blocked on a full queue")
	}
	arbiter.Stop()
}

func TestBlockedRuntimeWriterDoesNotBlockTerminalErrorPath(t *testing.T) {
	writer := &blockingRuntimeWriter{entered: make(chan struct{}), release: make(chan struct{})}
	previous := newRealFirstStageWithRuntimeSink
	newRealFirstStageWithRuntimeSink = func(sink application.RuntimeEventSink) (realFirstStage, error) {
		return &errorRuntimeStage{sink: sink, entered: writer.entered}, nil
	}
	defer func() { newRealFirstStageWithRuntimeSink = previous }()

	result := make(chan int, 1)
	go func() {
		var stdout bytes.Buffer
		result <- Run(context.Background(), []string{"collect", "--output", filepath.Join(t.TempDir(), "package"), "--runtime-status-stderr"}, &stdout, writer)
	}()
	select {
	case code := <-result:
		if code != ExitProviderError {
			t.Fatalf("terminal code=%d, want %d", code, ExitProviderError)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("terminal error path waited on blocked stderr")
	}
	close(writer.release)
}

func TestBlockedRuntimeWriterStopIsBounded(t *testing.T) {
	writer := &blockingRuntimeWriter{entered: make(chan struct{}), release: make(chan struct{})}
	arbiter := newStderrArbiter(writer)
	_, _ = arbiter.Write([]byte("blocked\n"))
	select {
	case <-writer.entered:
	case <-time.After(time.Second):
		t.Fatal("arbiter writer did not start")
	}
	start := time.Now()
	arbiter.Stop()
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("arbiter Stop waited for blocked writer")
	}
	close(writer.release)
}

func TestRuntimeFlagIsOptInStderrOnlyAndPreservesFinalStdout(t *testing.T) {
	result := fakeRealResult(application.RunComplete, "", execution.Available, true, execution.Collected, execution.ReasonNone)
	previousNormal := newRealFirstStage
	previousRuntime := newRealFirstStageWithRuntimeSink
	defer func() {
		newRealFirstStage = previousNormal
		newRealFirstStageWithRuntimeSink = previousRuntime
	}()
	newRealFirstStage = func() (realFirstStage, error) { return &fakeRealFirstStage{result: result}, nil }
	newRealFirstStageWithRuntimeSink = func(sink application.RuntimeEventSink) (realFirstStage, error) {
		return &runtimeEmittingStage{sink: sink, result: result}, nil
	}

	output := filepath.Join(t.TempDir(), "package")
	var normalOut, normalErr bytes.Buffer
	if code := Run(context.Background(), []string{"collect", "--output", output}, &normalOut, &normalErr); code != ExitOK {
		t.Fatalf("normal code=%d stderr=%q", code, normalErr.String())
	}
	var runtimeOut, runtimeErr bytes.Buffer
	if code := Run(context.Background(), []string{"collect", "--output", output, "--runtime-status-stderr"}, &runtimeOut, &runtimeErr); code != ExitOK {
		t.Fatalf("runtime code=%d stderr=%q", code, runtimeErr.String())
	}
	if normalErr.Len() != 0 || strings.Contains(normalErr.String(), runtimeStatusPrefix) || !bytes.Equal(normalOut.Bytes(), runtimeOut.Bytes()) {
		t.Fatalf("opt-in changed normal contract: normal stdout=%q stderr=%q runtime stdout=%q", normalOut.String(), normalErr.String(), runtimeOut.String())
	}
	lines := strings.Split(strings.TrimSpace(runtimeErr.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("runtime stderr lines=%q", lines)
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, runtimeStatusPrefix) || strings.Contains(runtimeOut.String(), runtimeStatusPrefix) {
			t.Fatalf("runtime carrier was not stderr-only: stdout=%q stderr=%q", runtimeOut.String(), runtimeErr.String())
		}
	}
}

func TestSyntheticRuntimeFlagFailsBeforeExecution(t *testing.T) {
	output := filepath.Join(t.TempDir(), "must-not-exist")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"collect", "--synthetic", "available-collected", "--output", output, "--runtime-status-stderr"}, &stdout, &stderr)
	if code != ExitUsage || stdout.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("synthetic output was touched: err=%v", err)
	}
}

func TestRuntimeWriterFailureDoesNotChangeFinalResult(t *testing.T) {
	result := fakeRealResult(application.RunComplete, "", execution.Available, true, execution.Collected, execution.ReasonNone)
	previous := newRealFirstStageWithRuntimeSink
	newRealFirstStageWithRuntimeSink = func(sink application.RuntimeEventSink) (realFirstStage, error) {
		return &runtimeEmittingStage{sink: sink, result: result}, nil
	}
	defer func() { newRealFirstStageWithRuntimeSink = previous }()
	var stdout bytes.Buffer
	stderr := failingTerminalWriterPointer(0, errors.New("injected runtime writer failure"))
	code := Run(context.Background(), []string{"collect", "--output", filepath.Join(t.TempDir(), "package"), "--runtime-status-stderr"}, &stdout, stderr)
	if code != ExitOK {
		t.Fatalf("code=%d stdout=%q", code, stdout.String())
	}
	var summary realCollectSummary
	if err := json.Unmarshal(stdout.Bytes(), &summary); err != nil || summary.RunState != application.RunComplete || !summary.FinalizationVerified {
		t.Fatalf("final stdout lost authority: summary=%+v err=%v", summary, err)
	}
}

type panicWriter struct{}

func (panicWriter) Write([]byte) (int, error) { panic("injected writer panic") }

func failingTerminalWriterPointer(limit int, err error) *failingTerminalWriter {
	return &failingTerminalWriter{limit: limit, err: err}
}
