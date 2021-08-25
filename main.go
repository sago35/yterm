package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"go.bug.st/serial"
)

func main() {
	port := flag.String("port", "", "serial port")
	flag.Parse()

	if *port == "" {
		fmt.Printf("usage: %s --port PORT\n", os.Args[0])
		os.Exit(0)
	}

	err := run(*port)
	if err != nil {
		log.Fatal(err)
	}
}

func run(p string) error {
	port, err := serial.Open(p, &serial.Mode{})
	if err != nil {
		return err
	}
	defer port.Close()

	buf := make([]byte, 1000)
	for {
		n, err := port.Read(buf)
		if err != nil {
			return err
		}

		if n == 0 {
			fmt.Printf("\nEOF")
			break
		}

		fmt.Printf("%v", string(buf[:n]))
	}
	return nil
}
