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
					conn, err := net.DialTimeout("unix", "/run/rpcbind.sock", 1*time.Second)
					if err == nil {
						conn.Close()
						log.Println("Socket /run/rpcbind.sock found and confirmed as rpcbind.")
						return
					} else {
						log.Println("Unable to connect to the socket, continuing to wait...")
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
