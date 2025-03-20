/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

/*
Copyright (c) Advanced Micro Devices, Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the \"License\");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an \"AS IS\" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/ROCm/gpu-operator/tests/e2e/utils"

	log "github.com/sirupsen/logrus"
)

func fnHealth(w http.ResponseWriter, r *http.Request) {
	_, err := w.Write([]byte("healthy"))
	if err != nil {
		log.Errorf("response write error: %v\n", err)
	}
}

func fnRunCommand(w http.ResponseWriter, r *http.Request) {
	var req utils.UserRequest
	if r.Method != http.MethodPost {
		_, err := w.Write([]byte("requires request type POST for cmd execution"))
		if err != nil {
			log.Errorf("response write error: %v\n", err)
		}
		return
	}
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		if _, e := w.Write([]byte(fmt.Sprintf("command not found : %v", err))); e != nil {
			log.Errorf("response write error: %v\n", e)
		}
		return
	}
	reqBody := req.Command
	cmd := exec.Command("bash", "-c", string(reqBody))
	output, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		log.Errorf("Command %v failed to start with error: %v", string(reqBody), err)
		return
	}

	var buffer bytes.Buffer
	scanner := bufio.NewScanner(output)
	for scanner.Scan() {
		buffer.WriteString(scanner.Text())
	}
	if err := cmd.Wait(); err != nil {
		if _, e := w.Write([]byte(fmt.Sprintf("Command %v did not complete with error: %v", string(reqBody), err))); e != nil {
			log.Errorf("response write error: %v\n", e)
		}
		return
	}

	if _, e := w.Write(buffer.Bytes()); e != nil {
		log.Errorf("response write error: %v\n", e)
	}
}

func fnReboot(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPost {
		_, err := w.Write([]byte("requires request type POST for reboot"))
		if err != nil {
			log.Errorf("response write error: %v\n", err)
		}
		return
	}

	rebootCmd, err := exec.LookPath("reboot")
	if err != nil {
		_, err := w.Write([]byte(fmt.Sprintf("reboot find error: %v", err)))
		if err != nil {
			log.Errorf("response write error: %v\n", err)
		}
		return
	}

	// Execute the reboot command
	cmd := exec.Command(rebootCmd)
	_, err = w.Write([]byte("rebooting the system"))
	if err != nil {
		log.Errorf("response write error: %v\n", err)
	}

	go func() {
		time.Sleep(2 * time.Second)
		err := cmd.Run()
		if err != nil {
			_, rerr := w.Write([]byte(fmt.Sprintf("reboot failed error: %v", err)))
			if rerr != nil {
				log.Errorf("response failed: %v\n", err)
				log.Errorf("response write error: %v\n", rerr)
			}
			return
		}
	}()
}

func main() {

	// Create a channel to listen for OS signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Create a context that will be canceled when an OS signal is received
	_, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Create a new ServeMux
	mux := http.NewServeMux()
	mux.HandleFunc("/health", fnHealth)
	mux.HandleFunc("/reboot", fnReboot)
	mux.HandleFunc("/runcommand", fnRunCommand)

	// Create the HTTP server
	server := &http.Server{
		Addr:    fmt.Sprintf(":%s", utils.HttpServerPort),
		Handler: mux,
	}

	// Start the server in a goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Errorf("listen on %s failed. error: %v\n", server.Addr, err)
		}
	}()
	log.Infof("Server listening at: %s", server.Addr)

	// Wait for an OS signal
	<-sigChan
	log.Infof("interrupted , shutting down...")

	// Create a context with a timeout for the shutdown process
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Attempt to gracefully shutdown the server
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Errorf("Server shutdown failed. error: %v", err)
	}

	log.Infof("Server exiting...")
}
