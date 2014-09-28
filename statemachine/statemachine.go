package statemachine

import (
	"log"
	"os"
)

type Handler func() string

type Machine struct {
	Handlers map[string]Handler
	Logger   *log.Logger
}

type StateMachineError struct {
	State string
}

func (sme StateMachineError) Error() string {
	return "statemachine: No handler function registered for state: " + sme.State
}

func NewMachine() Machine {
	return Machine{
		Handlers: map[string]Handler{},
		Logger:   log.New(os.Stdout, "statemachine: ", 0),
	}
}

func (machine Machine) AddState(stateName string, handlerFn Handler) {
	machine.Handlers[stateName] = handlerFn
}

func (machine Machine) Run() (success bool, error error) {
	state := "INIT"
	machine.Logger.Println("Starting in state: INIT")
	for {
		if handler, present := machine.Handlers[state]; present {
			oldstate := state
			state = handler()
			machine.Logger.Printf("State transition: %s -> %s\n", oldstate, state)
			if state == "END" {
				machine.Logger.Println("Terminating")
				return true, nil
			}
		} else {
			return false, StateMachineError{state}
		}
	}
}
