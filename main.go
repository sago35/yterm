package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"time"

	"go.bug.st/serial"
	"golang.org/x/crypto/ssh/terminal"
	"golang.org/x/term"
)

func main() {
	port := flag.String("port", "", "serial port")
	disableCtrlC := flag.Bool("disable-ctrl-c", false, "disable ctrl-c")
	flag.Parse()

	if *port == "" {
		fmt.Printf("usage: %s --port PORT\n", os.Args[0])
		os.Exit(0)
	}

	err := run(*port, *disableCtrlC)
	if err != nil {
		log.Fatal(err)
	}
}

func run(p string, disableCtrlC bool) error {
	if disableCtrlC {
		fmt.Printf("`Ctrl-x q` to exit\n")
	}

	var mu sync.Mutex

	port, err := serial.Open(p, &serial.Mode{})

	for i := 0; i < 50; i++ {
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
		port, err = serial.Open(p, &serial.Mode{})
	}

	if err != nil {
		return err
	}
	defer port.Close()

	err = port.SetReadTimeout(1 * time.Second)
	if err != nil {
		return err
	}

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return err
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)

	go func() {
		<-sig
		terminal.Restore(int(os.Stdin.Fd()), oldState)
		os.Exit(0)
	}()

	portHasError := false
	go func() {
		buf := make([]byte, 1000)
		for {
			n, err := port.Read(buf)
			if err != nil {
				mu.Lock()
				portHasError = true
				mu.Unlock()
				for err != nil {
					port, err = serial.Open(p, &serial.Mode{})
					time.Sleep(100 * time.Millisecond)
				}
				mu.Lock()
				portHasError = false
				mu.Unlock()
				continue
			}

			if n == 0 {
				continue
			}
			fmt.Printf("%v", string(buf[:n]))
		}
	}()

	buf := make([]byte, 1000)
	prev := byte(0)

LOOP:
	for {
		n, _ := os.Stdin.Read(buf)
		for _, b := range buf[:n] {
			if !disableCtrlC {
				if b == 0x03 {
					break LOOP
				}
			}
			if prev == 0x18 && b == 'q' {
				break LOOP
			}
			prev = b
		}
		mu.Lock()
		if !portHasError {
			port.Write(buf[:n])
		}
		mu.Unlock()
	}

	return nil
}
