/*
Copyright 2024 Flant JSC

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

package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	log.Println("Waiting for socket /run/rpcbind.sock...")

	sigs := make(chan os.Signal, 1)
	done := make(chan string, 1)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigs
		log.Printf("Received signal %s, exiting.", sig)
		done <- sig.String()
	}()

	for {
		select {
		case sigName := <-done:
			log.Printf("Program terminated by %s signal.", sigName)
			return
		default:
			info, err := os.Lstat("/run/rpcbind.sock")
			if err == nil {
				if (info.Mode() & os.ModeSocket) != 0 {
					err := checkRpcbind("/run/rpcbind.sock")
					if err == nil {
						log.Println("Socket /run/rpcbind.sock found and confirmed as rpcbind.")
						return
					} else {
						log.Printf("Socket check failed: %v", err)
					}
				} else {
					log.Println("/run/rpcbind.sock found but is not a socket. Continuing to wait...")
				}
			} else if os.IsNotExist(err) {
				log.Println("/run/rpcbind.sock does not exist, continuing to wait...")
			} else {
				log.Printf("Error checking socket: %v", err)
			}
			time.Sleep(1 * time.Second)
		}
	}
}

// checkRpcbind attempts to perform an RPC NULL call to rpcbind to confirm it's running
func checkRpcbind(socketPath string) error {
	conn, err := net.DialTimeout("unix", socketPath, 1*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Create a RPC NULL call message
	msg, err := createRpcNullCall()
	if err != nil {
		return err
	}

	// Send the message
	_, err = conn.Write(msg)
	if err != nil {
		return err
	}

	// Set a read deadline
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	// Read the response
	response := make([]byte, 1024)
	n, err := conn.Read(response)
	if err != nil {
		return err
	}

	// Basic validation of response
	if n < 24 {
		return errors.New("response too short to be valid")
	}

	return nil
}

// createRpcNullCall constructs an RPC message for the NULL procedure call
func createRpcNullCall() ([]byte, error) {
	var buf bytes.Buffer

	// XID (transaction ID)
	if err := binary.Write(&buf, binary.BigEndian, uint32(0x12345678)); err != nil {
		return nil, err
	}

	// Message type: Call (0)
	if err := binary.Write(&buf, binary.BigEndian, uint32(0)); err != nil {
		return nil, err
	}

	// RPC version (2)
	if err := binary.Write(&buf, binary.BigEndian, uint32(2)); err != nil {
		return nil, err
	}

	// Program number (100000 for portmapper)
	if err := binary.Write(&buf, binary.BigEndian, uint32(100000)); err != nil {
		return nil, err
	}

	// Program version (2)
	if err := binary.Write(&buf, binary.BigEndian, uint32(2)); err != nil {
		return nil, err
	}

	// Procedure number (0 for NULL)
	if err := binary.Write(&buf, binary.BigEndian, uint32(0)); err != nil {
		return nil, err
	}

	// Credentials (AUTH_NULL)
	if err := binary.Write(&buf, binary.BigEndian, uint32(0)); err != nil {
		return nil, err
	}
	// Credentials length
	if err := binary.Write(&buf, binary.BigEndian, uint32(0)); err != nil {
		return nil, err
	}

	// Verifier (AUTH_NULL)
	if err := binary.Write(&buf, binary.BigEndian, uint32(0)); err != nil {
		return nil, err
	}
	// Verifier length
	if err := binary.Write(&buf, binary.BigEndian, uint32(0)); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
