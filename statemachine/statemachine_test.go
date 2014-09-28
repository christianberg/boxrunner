package statemachine

import "testing"

func TestSimplestStateMachine(t *testing.T) {
	m := NewMachine()
	m.AddState("INIT", func() string { return "END" })
	success, err := m.Run()
	if !success {
		t.Error("State machine did not run successfully")
	}
	if err != nil {
		t.Error("State machine returned error")
	}
}

func TestUnknownState(t *testing.T) {
	m := NewMachine()
	m.AddState("INIT", func() string { return "BAR" })
	success, err := m.Run()
	if success != false {
		t.Error("State machine signalled success despite unknown state")
	}
	if err.Error() != "statemachine: No handler function registered for state: BAR" {
		t.Error("State machine didn't return correct error")
	}
}

func ExampleThreeStateMachine() {
	m := NewMachine()
	m.AddState("INIT", func() string { return "FOO" })
	m.AddState("FOO", func() string { return "END" })
	m.Run()
	// Output:
	// statemachine: Starting in state: INIT
	// statemachine: State transition: INIT -> FOO
	// statemachine: State transition: FOO -> END
	// statemachine: Terminating
}
