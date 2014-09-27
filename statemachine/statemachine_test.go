package statemachine

import "testing"

func TestSimplestStateMachine(t *testing.T) {
	m := NewMachine()
	m.AddState("INIT", func() string { return "END" })
	m.Run()
}

func TestThreeStateMachine(t *testing.T) {
	m := NewMachine()
	m.AddState("INIT", func() string { return "FOO" })
	m.AddState("FOO", func() string { return "END" })
	m.Run()
}
