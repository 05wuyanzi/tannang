//go:build !windows

package provider

import (
	"errors"

	"github.com/05wuyanzi/tannang/internal/execution"
)

type unsupportedEventLogAPI struct{}

func newPlatformEventLogAPI() eventLogAPI { return unsupportedEventLogAPI{} }

func (unsupportedEventLogAPI) availability() (execution.Reason, error) {
	return execution.ReasonUnsupportedOS, errors.New("Windows Event Log requires Windows")
}
func (unsupportedEventLogAPI) openLog(string) (uintptr, error) {
	return 0, &eventLogFailure{reason: execution.ReasonUnsupportedOS, err: errors.New("Windows Event Log requires Windows")}
}
func (unsupportedEventLogAPI) exportLog(string, string, string, uint32) error {
	return &eventLogFailure{reason: execution.ReasonUnsupportedOS, err: errors.New("Windows Event Log requires Windows")}
}
func (unsupportedEventLogAPI) close(uintptr) error {
	return &eventLogFailure{reason: execution.ReasonUnsupportedOS, err: errors.New("Windows Event Log requires Windows")}
}
