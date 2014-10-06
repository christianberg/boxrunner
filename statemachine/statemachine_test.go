package statemachine

import (
	"fmt"
	"testing"
)

func compareChannelWithSlice(actual chan string, expected []string) (bool, string) {
	for s := range actual {
		if s != expected[0] {
			return false, fmt.Sprintf("Wrong state encountered. Expected %v, got %v", expected[0], s)
		}
		expected = expected[1:]
	}
	if len(expected) > 0 {
		return false, fmt.Sprintf("State machine terminated early, %v expected states not reached", len(expected))
	}
	return true, ""
}

func TestSimplestStateMachine(t *testing.T) {
	m := NewMachine()
	m.StateLog = make(chan string, 10)
	m.AddState("INIT", func() string { return "END" })
	success, err := m.Run()
	if !success {
		t.Error("State machine did not run successfully")
	}
	if err != nil {
		t.Error("State machine returned error")
	}
	if success, err := compareChannelWithSlice(m.StateLog, []string{"END"}); !success {
		t.Error(err)
	}
}

func TestUnknownState(t *testing.T) {
	m := NewMachine()
	m.AddState("INIT", func() string { return "BAR" })
	success, err := m.Run()
	if success != false {
		t.Error("State machine signalled success despite unknown state")
	}
	if err.Error() != "ERROR: No handler function registered for state: BAR" {
		t.Error("State machine didn't return correct error")
	}
}

func TestTickerAllowsToSyncStateMachine(t *testing.T) {
	m := NewMachine()
	m.AddState("INIT", func() string { return "FOO" })
	m.AddState("FOO", func() string { return "END" })
	ticker := make(chan bool)
	log := make(chan string, 10)
	m.Ticker = ticker
	m.StateLog = log
	go m.Run()

	if s:= <-log; s != "FOO" {
		t.Errorf("Expected first state to be FOO, was %v", s)
	}
	select {
	case <-log:
		t.Errorf("New state was reached before ticker triggered")
	default:
		ticker <- true
	}
	if s:= <-log; s != "END" {
		t.Errorf("Expected state after tick to be END, was %v", s)
	}
}

func ExampleThreeStateMachine() {
	m := NewMachine()
	m.AddState("INIT", func() string { return "FOO" })
	m.AddState("FOO", func() string { return "END" })
	m.Run()
	// Output:
	// statemachine: INFO: Starting in state: INIT
	// statemachine: INFO: State transition: INIT -> FOO
	// statemachine: INFO: State transition: FOO -> END
	// statemachine: INFO: Terminating
}
