package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/mistifyio/acomm"
	flags "github.com/spf13/pflag"
)

const (
	argSep = ":"
)

func main() {
	log.SetLevel(log.FatalLevel)

	var coordinator, responseAddr, taskName string
	var taskArgs []string
	flags.StringVarP(&coordinator, "coordinator_url", "c", "", "url of the coordinator")
	flags.StringVarP(&taskName, "task", "t", "", "task to run")
	flags.StringSliceVarP(&taskArgs, "request_arg", "a", []string{}, fmt.Sprintf("task specific argument the form 'key%svalue'. can be set multiple times", argSep))
	flags.StringVarP(&responseAddr, "response_addr", "r", ":4080", "address for response http handler to listen on")
	flags.Parse()

	args, err := parseTaskArgs(taskArgs)
	dieOnError(err)

	result, respErr, err := startResponseServer(responseAddr)
	dieOnError(err)

	dieOnError(makeRequest(coordinator, taskName, responseAddr, args))

	select {
	case err := <-respErr:
		dieOnError(err)
	case result := <-result:
		j, _ := json.Marshal(result)
		fmt.Println(string(j))
	}
}

func dieOnError(err error) {
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func parseTaskArgs(taskArgs []string) (map[string]string, error) {
	out := make(map[string]string)
	for _, in := range taskArgs {
		parts := strings.Split(in, argSep)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid request arg: '%s'", in)
		}
		out[parts[0]] = parts[1]
	}
	return out, nil
}

func startResponseServer(addr string) (chan interface{}, chan error, error) {
	result := make(chan interface{}, 1)
	errChan := make(chan error, 1)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			errChan <- err
			return
		}

		resp := &acomm.Response{}
		if err := json.Unmarshal(body, resp); err != nil {
			errChan <- err
			return
		}

		ack, _ := json.Marshal(&acomm.Response{})
		_, _ = w.Write(ack)

		if resp.Error != nil {
			errChan <- resp.Error
			return
		}

		result <- resp.Result
	})

	runErr := make(chan error)
	running := time.NewTimer(time.Second)
	go func() {
		if err := http.ListenAndServe(addr, nil); err != nil {
			runErr <- err
		}
	}()

	var err error
	select {
	case <-running.C:
	case err = <-runErr:
	}

	return result, errChan, err
}

func makeRequest(coordinator, taskName, responseAddr string, taskArgs map[string]string) error {
	coordinatorURL, err := url.ParseRequestURI(coordinator)
	if err != nil {
		return errors.New("invalid coordinator url")
	}

	responseHook := fmt.Sprintf("http://%s/", responseAddr)
	req, err := acomm.NewRequest(taskName, responseHook, taskArgs, nil, nil)
	if err != nil {
		return err
	}

	return acomm.Send(coordinatorURL, req)
}