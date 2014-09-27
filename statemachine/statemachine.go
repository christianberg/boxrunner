package statemachine

type Handler func() string

type Machine struct {
	Handlers map[string]Handler
}

type StateMachineError struct {
	State string
}

func (sme StateMachineError) Error() string {
	return "statemachine: No handler function registered for state: " + sme.State
}

func NewMachine() Machine {
	return Machine{
		map[string]Handler{},
	}
}

func (machine Machine) AddState(stateName string, handlerFn Handler) {
	machine.Handlers[stateName] = handlerFn
}

func (machine Machine) Run() (success bool, error error) {
	state := "INIT"
	for {
		if handler, present := machine.Handlers[state]; present {
			state = handler()
			if state == "END" {
				return true, nil
			}
		} else {
			return false, StateMachineError{state}
		}
	}
}
