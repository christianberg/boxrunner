prepare:
	go get github.com/hashicorp/consul/api
	go get github.com/fsouza/go-dockerclient

docs: docs/state_machine.png

%.png: %.dot
	dot -Tpng -o $@ $<
