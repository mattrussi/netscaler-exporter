package main

import (
	"sync"
)

// TaskID defines the differents tasks available working with the Nitro API.
type TaskID int

const (
	nitroTaskAPI TaskID = iota
	nitroTaskRaw
	nitroTaskData
	nitroProm
)

var nitroTaskStrings = [...]string{
	`nitroTaskAPI`,
	`nitroTaskRaw`,
	`nitroTaskData`,
	`nitroProm`,
}

// ID returns the int ID.
func (t TaskID) ID() int {
	return int(t)
}

// String returns the definition of the TaskID.
func (t TaskID) String() string {
	return nitroTaskStrings[t]
}

// NitroData identifies the type of resource or stat from the Nitro API.
type NitroData interface {
	NitroType() string
}

// NitroRaw represents raw data returned by the Nitro API.
type NitroRaw interface {
	Len() int
}

// RawData is any payload as returned by the Nitro API.
type RawData []byte

// Len returns the size of the underlying []byte.
func (r RawData) Len() int {
	return len(r)
}

type metricHandleFunc func(*Pool, *sync.WaitGroup)

func defaultMetricHandleFunc(P *Pool, wg *sync.WaitGroup) {
	wg.Done()
}

var metricsMap = map[string]metricHandleFunc{
	servicesSubsystem: processSvcStats,
	nsSubsystem:       processNSStats,
}

func (p *Pool) collectMetrics(wg *sync.WaitGroup) {
	if wg != nil {
		defer wg.Done()
	}
	switch {
	case p.stopped:
		p.logger.Info("unable to collect metrics, process is stopping")
	case p.flipBit.good():
		defer p.flipBit.flip()
		for _, f := range p.metricHandlers {
			f(p, nil)
		}
	default:
		p.logger.Info(("metric collection already in progress"))
	}
}

// CurState is the current state as returned by the Nitro API.
type CurState string

// Value returns the value mapping for the CurState.
func (c CurState) Value() float64 {
	switch c {
	case `DOWN`:
		return 0.0
	case `UP`:
		return 1.0
	case `OUT OF SERVICE`:
		return 2.0
	default:
		return 3.0
	}
}
