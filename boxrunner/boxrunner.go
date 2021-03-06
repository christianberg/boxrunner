package boxrunner

import (
	"fmt"
	"github.com/christianberg/boxrunner/statemachine"
	"github.com/fsouza/go-dockerclient"
	"github.com/hashicorp/consul/api"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

type BoxRunner struct {
	Service  string
	ID       string
	port     string
	consul   *api.Client
	logger   *log.Logger
	dock     *docker.Client
	lock     *api.KVPair
	failwait time.Duration
	lastfail time.Time
}

type BoxRunnerOptions struct {
	ConsulAddress string
	ConsulClient  *api.Client
	Logger        *log.Logger
}

var DefaultOptions = &BoxRunnerOptions{
	ConsulAddress: "localhost:8500",
}

func createDockerClient() (*docker.Client, error) {
	endpoint := os.Getenv("DOCKER_HOST")
	path := os.Getenv("DOCKER_CERT_PATH")
	ca := fmt.Sprintf("%s/ca.pem", path)
	cert := fmt.Sprintf("%s/cert.pem", path)
	key := fmt.Sprintf("%s/key.pem", path)
	client, err := docker.NewTLSClient(endpoint, cert, key, ca)
	return client, err
}

func NewBoxRunner(service_name string, options *BoxRunnerOptions) (runner *BoxRunner, err error) {
	if options == nil {
		options = DefaultOptions
	}
	completeOptions(options)

	logger := options.Logger
	dock, err := createDockerClient()
	if err != nil {
		logger.Printf("Could not initialize Docker client: %s", err)
		return
	}

	hostname, err := os.Hostname()
	if err != nil {
		logger.Printf("Could not determine hostname: %s", err)
		return
	}

	port := os.Getenv("PORT")
	runner_id := fmt.Sprintf("boxrunner-%v-%v", hostname, port)
	logger.Printf("This is boxrunner: %v", runner_id)

	runner = &BoxRunner{
		Service: service_name,
		ID:      runner_id,
		consul:  options.ConsulClient,
		logger:  options.Logger,
		dock:    dock,
		port:    port,
		lock: &api.KVPair{
			Key:   service_name,
			Value: []byte(runner_id),
		},
		failwait: 1 * time.Second,
	}
	return
}

func (b *BoxRunner) findOrCreateCheck() error {
	checks, err := b.consul.Agent().Checks()
	if err != nil {
		b.logger.Printf("Could not list existing checks: %v", err)
		return err
	}
	for _, check := range checks {
		if check.CheckID == b.ID {
			b.logger.Printf("Found existing check: %v\n", check.CheckID)
			return nil
		}
	}

	err = b.consul.Agent().CheckRegister(&api.AgentCheckRegistration{
		Name: b.ID,
		ID:   b.ID,
		AgentServiceCheck: api.AgentServiceCheck{
			HTTP:     fmt.Sprintf("http://localhost:%v/health", b.port),
			Interval: "5s",
		},
	})
	if err != nil {
		b.logger.Printf("ERROR: Could not register boxrunner healthcheck: %v", err)
	}
	return err
}

func (b *BoxRunner) findOrCreateSession() (string, error) {
	sessions, _, err := b.consul.Session().List(nil)
	if err != nil {
		b.logger.Printf("Could not list existing sessions: %v", err)
		return "", err
	}
	for _, session := range sessions {
		if session.Name == b.ID {
			b.logger.Printf("Found existing session: %v\n", session.ID)
			return session.ID, nil
		}
	}

	b.findOrCreateCheck()

	session_entry := &api.SessionEntry{
		Name:      b.ID,
		LockDelay: 5 * time.Second,
		Checks:    []string{"serfHealth", b.ID},
	}
	session_id, _, err := b.consul.Session().Create(session_entry, nil)
	if err != nil {
		b.logger.Printf("ERROR: Could not create session: %v", err)
		return "", err
	}
	b.logger.Printf("INFO: Session created (ID: %v)\n", session_id)
	return session_id, nil
}

func (b *BoxRunner) waitForLockChange(predicate func(string) bool) (err error) {
	query_options := &api.QueryOptions{
		WaitIndex: 0,
	}
	for {
		lock_status, meta, err := b.consul.KV().Get(b.lock.Key, query_options)
		if err != nil {
			b.logger.Printf("ERROR: Cannot check lock status: %s", err)
			return err
		}
		if lock_status == nil || predicate(lock_status.Session) {
			b.logger.Println("INFO: Lock was released")
			return nil
		}
		query_options.WaitIndex = meta.LastIndex
	}
}

func (b *BoxRunner) Run() (success bool, error error) {
	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "OK")
	})

	go http.ListenAndServe("127.0.0.1:"+b.port, nil)

	machine := &statemachine.Machine{
		Handlers: map[string]statemachine.Handler{},
		Logger:   b.logger,
	}

	machine.AddState("INIT", func() string {
		return "DISCOVER"
	})

	machine.AddState("DISCOVER", func() string {
		session_id, err := b.findOrCreateSession()
		if err != nil {
			return "FAILED"
		}
		b.lock.Session = session_id

		if err := b.dock.Ping(); err != nil {
			b.logger.Printf("ERROR: Cannot ping docker server: %v", err)
			return "FAILED"
		}
		return "COMPETE"
	})

	machine.AddState("COMPETE", func() string {
		b.logger.Printf("INFO: Trying to acquire lock for %s\n", b.Service)
		success, _, err := b.consul.KV().Acquire(b.lock, nil)
		if err != nil {
			b.logger.Printf("ERROR: Could not acquire lock: %s", err)
			return "FAILED"
		}
		if success {
			b.logger.Println("INFO: Lock acquired!")
			return "START"
		} else {
			lock_status, _, _ := b.consul.KV().Get(b.lock.Key, nil)
			if lock_status != nil {
				b.logger.Printf("INFO: Lock is already taken by: %s", lock_status.Value)
			}
			return "SLEEP"
		}
	})

	machine.AddState("START", func() string {
		if err := b.dock.PullImage(
			docker.PullImageOptions{
				Repository: "busybox",
			},
			docker.AuthConfiguration{},
		); err != nil {
			b.logger.Printf("ERROR: Failed to pull image: %v", err)
			return "FAILED"
		}
		b.logger.Println("INFO: Image pulled")

		container, err := b.dock.CreateContainer(
			docker.CreateContainerOptions{
				Name: b.ID,
				Config: &docker.Config{
					Image: "busybox",
					Cmd:   []string{"/bin/sleep", "10"},
				},
			},
		)
		if err != nil {
			b.logger.Printf("ERROR: Failed to create docker container: %v", err)
			return "FAILED"
		}
		b.logger.Printf("INFO: Container created: %v", container.ID)

		if err := b.dock.StartContainer(container.ID, &docker.HostConfig{}); err != nil {
			b.logger.Printf("ERROR: Failed to start docker container: %v", err)
			return "FAILED"
		}
		b.logger.Printf("INFO: Container started: %v", container.ID)
		return "RUNNING"
	})

	machine.AddState("RUNNING", func() string {
		lock_watch := make(chan string)
		cancel_lock_watch := make(chan bool, 1)
		go func() {
			err := b.waitForLockChange(func(s string) bool { return s != b.lock.Session })
			select {
			case <-cancel_lock_watch:
				return
			default:
				if err != nil {
					lock_watch <- "FAILED"
					return
				}
				lock_watch <- "STOP"
				return
			}
		}()

		container_watch := make(chan string)
		cancel_container_watch := make(chan bool, 1)
		go func() {
			exit_code, err := b.dock.WaitContainer(b.ID)
			select {
			case <-cancel_container_watch:
				return
			default:
				if err != nil {
					b.logger.Printf("ERROR: Waiting for Docker container exit failed: %v", err)
					container_watch <- "FAILED"
					return
				}
				level := "INFO"
				if exit_code > 0 {
					level = "WARN"
				}
				b.logger.Printf("%v: Docker container exited with code %v\n", level, exit_code)
				container_watch <- "RELEASE"
				return
			}
		}()

		select {
		case s := <-lock_watch:
			cancel_container_watch <- true
			return s
		case s := <-container_watch:
			cancel_lock_watch <- true
			return s
		}
	})

	machine.AddState("SLEEP", func() string {
		time.Sleep(3 * time.Second)
		err := b.waitForLockChange(func(s string) bool { return s == "" })
		if err != nil {
			return "FAILED"
		}
		return "COMPETE"
	})

	machine.AddState("STOP", func() string {
		return "RELEASE"
	})

	machine.AddState("RELEASE", func() string {
		b.logger.Printf("INFO: Releasing lock for %s\n", b.Service)
		_, _, err := b.consul.KV().Release(b.lock, nil)
		if err != nil {
			b.logger.Printf("ERROR: Could not release lock: %v", err)
			return "FAILED"
		}
		return "REMOVE"
	})

	machine.AddState("REMOVE", func() string {
		err := b.dock.RemoveContainer(docker.RemoveContainerOptions{ID: b.ID})
		if err != nil {
			b.logger.Printf("ERROR: Could not remove container: %v", err)
			return "FAILED"
		}
		return "COMPETE"
	})

	machine.AddState("FAILED", func() string {
		if b.lastfail.Add(2 * b.failwait).Before(time.Now()) {
			b.failwait = 1 * time.Second
		}
		b.lastfail = time.Now()
		b.logger.Printf("WARN: Waiting for %v", b.failwait)
		time.Sleep(b.failwait)
		b.failwait = 2 * b.failwait
		if b.failwait > 5*time.Minute {
			b.failwait = 5 * time.Minute
		}
		return "DISCOVER"
	})

	return machine.Run()
}

func completeOptions(options *BoxRunnerOptions) {
	if options.ConsulClient == nil {
		if options.ConsulAddress == "" {
			options.ConsulAddress = DefaultOptions.ConsulAddress
		}
		var err error
		options.ConsulClient, err = api.NewClient(api.DefaultConfig())
		if err != nil {
			panic("Failed to create consul-api Client")
		}
	}

	if options.Logger == nil {
		options.Logger = log.New(os.Stdout, "", 0)
	}
}
