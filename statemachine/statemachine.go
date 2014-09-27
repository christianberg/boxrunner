package statemachine

import "fmt"

type Handler func() string

type Machine struct {
	Handlers map[string]Handler
}

func NewMachine() Machine {
	return Machine{
		map[string]Handler{},
	}
}

func (machine Machine) AddState(stateName string, handlerFn Handler) {
	machine.Handlers[stateName] = handlerFn
}

func (machine Machine) Run() {
	state := "INIT"
	for {
		if handler, present := machine.Handlers[state]; present {
			state = handler()
			if state == "END" {
				break
			}
		} else {
			panic(fmt.Sprintf("No handler function registered for state: %v", state))
		}
	}
}
