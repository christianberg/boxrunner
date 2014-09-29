package boxrunner

import (
	"fmt"
	"github.com/armon/consul-api"
	"github.com/christianberg/boxrunner/statemachine"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

type BoxRunnerOptions struct {
	ConsulAddress string
	ConsulClient  *consulapi.Client
	Logger        *log.Logger
}

var DefaultOptions = &BoxRunnerOptions{
	ConsulAddress: "localhost:8500",
}

func Run(service_name string, options *BoxRunnerOptions) (success bool, error error) {
	if options == nil {
		options = DefaultOptions
	}
	completeOptions(options)

	consul := options.ConsulClient
	logger := options.Logger
	hostname, err := os.Hostname()
	port := os.Getenv("PORT")
	if err != nil {
		logger.Fatalf("Could not determine hostname: %s", err)
	}
	runner_id := fmt.Sprintf("boxrunner-%v-%v", hostname, port)
	logger.Printf("This is boxrunner: %v", runner_id)

	machine := &statemachine.Machine{
		Handlers: map[string]statemachine.Handler{},
		Logger:   options.Logger,
	}

	lock := &consulapi.KVPair{
		Key:   service_name,
		Value: []byte(runner_id),
	}

	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "OK")
	})

	go http.ListenAndServe(":"+port, nil)

	machine.AddState("INIT", func() string {
		err := consul.Agent().CheckRegister(&consulapi.AgentCheckRegistration{
			Name: runner_id,
			AgentServiceCheck: consulapi.AgentServiceCheck{
				Script:   fmt.Sprintf("curl -sf http://localhost:%v/health", port),
				Interval: "5s",
			},
		})
		if err != nil {
			logger.Printf("ERROR: Could not register boxrunner healthcheck: %v", err)
			return "FAILED"
		}
		session_entry := &consulapi.SessionEntry{
			Checks: []string{"serfHealth", runner_id},
		}
		session, _, err := consul.Session().Create(session_entry, nil)
		if err != nil {
			logger.Printf("ERROR: Could not create session: %v", err)
			return "FAILED"
		}
		logger.Printf("INFO: Session created (ID: %v)\n", session)
		lock.Session = session
		return "COMPETING"
	})

	machine.AddState("COMPETING", func() string {
		logger.Printf("INFO: Trying to acquire lock for %s\n", service_name)
		success, _, err := consul.KV().Acquire(lock, nil)
		if err != nil {
			logger.Printf("ERROR: Could not acquire lock: %s", err)
			return "FAILED"
		}
		if success {
			logger.Println("INFO: Lock acquired!")
			return "RUNNING"
		} else {
			lock_status, _, _ := consul.KV().Get(lock.Key, nil)
			if lock_status != nil {
				logger.Printf("INFO: Lock is already taken by: %s", lock_status.Value)
			}
			return "SLEEPING"
		}
	})

	machine.AddState("RUNNING", func() string {
		lock_watch := make(chan string)
		cancel := make(chan bool, 1)
		go func() {
			query_options := &consulapi.QueryOptions{
				WaitIndex: 0,
			}
			for {
				lock_status, meta, err := consul.KV().Get(lock.Key, query_options)
				select {
				case <-cancel:
					return
				default:
					logger.Println("DEBUG: Watch on lock has fired")
					if err != nil {
						logger.Printf("ERROR: Cannot check lock status: %s", err)
						lock_watch <- "FAILED"
						return
					}
					if lock_status == nil || lock_status.Session != lock.Session {
						logger.Println("WARN: Lost my lock!")
						lock_watch <- "COMPETING"
						return
					}
					query_options.WaitIndex = meta.LastIndex
				}
			}
		}()

		select {
		case s := <-lock_watch:
			return s
		case <-time.After(20 * time.Second):
			cancel <- true
			return "STOPPING"
		}
	})

	machine.AddState("SLEEPING", func() string {
		time.Sleep(3 * time.Second)
		query_options := &consulapi.QueryOptions{
			WaitIndex: 0,
		}
		for {
			lock_status, meta, err := consul.KV().Get(lock.Key, query_options)
			logger.Println("DEBUG: Watch on lock has fired")
			if err != nil {
				logger.Printf("ERROR: Cannot check lock status: %s", err)
				return "FAILED"
			}
			if lock_status == nil || lock_status.Session == "" {
				logger.Println("INFO: Lock was released by other host")
				return "COMPETING"
			}
			query_options.WaitIndex = meta.LastIndex
		}
	})

	machine.AddState("STOPPING", func() string {
		logger.Printf("INFO: Releasing lock for %s\n", service_name)
		success, _, err := consul.KV().Release(lock, nil)
		if err != nil || success == false {
			logger.Printf("ERROR: Could not release lock: %s", err)
			return "FAILED"
		}
		return "COMPETING"
	})

	return machine.Run()
}

func completeOptions(options *BoxRunnerOptions) {
	if options.ConsulClient == nil {
		if options.ConsulAddress == "" {
			options.ConsulAddress = DefaultOptions.ConsulAddress
		}
		var err error
		// options.ConsulClient, err = consulapi.NewClient(&consulapi.Config{
		// 	Address: options.ConsulAddress,
		// })
		options.ConsulClient, err = consulapi.NewClient(consulapi.DefaultConfig())
		if err != nil {
			panic("Failed to create consul-api Client")
		}
	}

	if options.Logger == nil {
		options.Logger = log.New(os.Stdout, "", 0)
	}
}
