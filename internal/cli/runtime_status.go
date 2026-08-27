// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package cli

import (
	"encoding/json"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/05wuyanzi/tannang/internal/application"
)

const (
	runtimeStatusPrefix    = "TANNANG_RUNTIME "
	maxRuntimeRecordBytes  = 512
	runtimeEventBuffer     = 64
	runtimeHeartbeat       = 5 * time.Second
	runtimeStopWait        = 100 * time.Millisecond
	maxDiagnosticLineBytes = 16 * 1024
)

// stderrArbiter is the opt-in runtime invocation's single bounded stderr
// handoff. Producers never write the underlying stream directly.
type stderrArbiter struct {
	writer   io.Writer
	queue    chan []byte
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
	stopping atomic.Bool
	disabled atomic.Bool
}

func newStderrArbiter(writer io.Writer) *stderrArbiter {
	arbiter := &stderrArbiter{
		writer: writer,
		queue:  make(chan []byte, runtimeEventBuffer),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go arbiter.run()
	return arbiter
}

func (a *stderrArbiter) Write(value []byte) (int, error) {
	if a == nil || len(value) == 0 {
		return len(value), nil
	}
	if len(value) > maxDiagnosticLineBytes || !a.tryEnqueue(value) {
		return len(value), nil
	}
	return len(value), nil
}

func (a *stderrArbiter) tryEnqueue(value []byte) bool {
	if a == nil || a.stopping.Load() || a.disabled.Load() {
		return false
	}
	copyValue := append([]byte(nil), value...)
	select {
	case a.queue <- copyValue:
		return true
	default:
		return false
	}
}

func (a *stderrArbiter) Stop() {
	if a == nil {
		return
	}
	a.stopping.Store(true)
	a.stopOnce.Do(func() { close(a.stop) })
	select {
	case <-a.done:
	case <-time.After(runtimeStopWait):
	}
}

func (a *stderrArbiter) run() {
	defer close(a.done)
	for {
		select {
		case value := <-a.queue:
			if !a.write(value) {
				return
			}
		case <-a.stop:
			for {
				select {
				case value := <-a.queue:
					if !a.write(value) {
						return
					}
				default:
					return
				}
			}
		}
	}
}

func (a *stderrArbiter) write(value []byte) (ok bool) {
	defer func() {
		if recover() != nil {
			a.disabled.Store(true)
			ok = false
		}
	}()
	written, err := a.writer.Write(value)
	if err != nil || written != len(value) {
		a.disabled.Store(true)
		return false
	}
	return true
}

type runtimeWireRecord struct {
	Type  string `json:"type"`
	Event string `json:"event"`
	At    string `json:"at"`
}

type runtimeReporter struct {
	writer   io.Writer
	now      func() time.Time
	events   chan application.RuntimeEvent
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
	disabled atomic.Bool
	dropped  atomic.Uint64
}

func newRuntimeReporter(writer io.Writer, cadence time.Duration, now func() time.Time) *runtimeReporter {
	if cadence <= 0 {
		cadence = runtimeHeartbeat
	}
	if now == nil {
		now = time.Now
	}
	reporter := &runtimeReporter{
		writer: writer,
		now:    now,
		events: make(chan application.RuntimeEvent, runtimeEventBuffer),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go reporter.run(cadence)
	return reporter
}

func (r *runtimeReporter) TryEmit(event application.RuntimeEvent) bool {
	if r == nil || r.disabled.Load() || event.Validate() != nil {
		return false
	}
	select {
	case r.events <- event:
		return true
	default:
		r.dropped.Add(1)
		return false
	}
}

func (r *runtimeReporter) Stop() {
	if r == nil {
		return
	}
	r.stopOnce.Do(func() { close(r.stop) })
	select {
	case <-r.done:
	case <-time.After(runtimeStopWait):
	}
}

func (r *runtimeReporter) run(cadence time.Duration) {
	defer close(r.done)
	ticker := time.NewTicker(cadence)
	defer ticker.Stop()
	heartbeatActive := false
	for {
		select {
		case event := <-r.events:
			r.write(event)
			if event.Type == application.RuntimeTypeStart && event.Event == application.RuntimeEventRunStarted {
				heartbeatActive = true
			}
			if event.Type == application.RuntimeTypeTerminal && event.Event == application.RuntimeEventRunReturned {
				heartbeatActive = false
			}
		case <-ticker.C:
			if heartbeatActive {
				r.write(application.RuntimeEvent{Type: application.RuntimeTypeHeartbeat, Event: application.RuntimeEventPulse})
			}
		case <-r.stop:
			for {
				select {
				case event := <-r.events:
					r.write(event)
				default:
					return
				}
			}
		}
	}
}

func (r *runtimeReporter) write(event application.RuntimeEvent) {
	defer func() {
		if recover() != nil {
			r.disabled.Store(true)
		}
	}()
	if r.disabled.Load() || event.Validate() != nil {
		return
	}
	at := r.now().UTC()
	if at.IsZero() {
		return
	}
	payload, err := json.Marshal(runtimeWireRecord{Type: event.Type, Event: event.Event, At: at.Format(time.RFC3339Nano)})
	if err != nil || len(payload) == 0 {
		return
	}
	line := make([]byte, 0, len(runtimeStatusPrefix)+len(payload)+1)
	line = append(line, runtimeStatusPrefix...)
	line = append(line, payload...)
	line = append(line, '\n')
	if len(line) > maxRuntimeRecordBytes {
		return
	}
	if written, err := r.writer.Write(line); err != nil || written != len(line) {
		r.disabled.Store(true)
	}
}
