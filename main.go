package main

import "github.com/christianberg/boxrunner/boxrunner"

func main() {
	runner, err := boxrunner.NewBoxRunner("foo",nil)
	if err != nil {
		panic("Could not create BoxRunner: " + err.Error())
	}
	runner.Run()
}
