package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
	"golang.org/x/crypto/ssh/terminal"
	"golang.org/x/term"
)

func main() {
	port := flag.String("port", "", "serial port")
	baud := flag.Int("baud", 115200, "baudrate")
	disableCtrlC := flag.Bool("disable-ctrl-c", false, "disable ctrl-c")
	target := flag.String("target", "", "target")
	flag.Parse()

	command := ""
	if len(os.Args) >= 2 {
		command = os.Args[1]
	}

	switch command {
	case "list":
		err := showPorts()
		if err != nil {
			log.Fatal(err)
		}
	default:
		err := run(*port, *target, *baud, *disableCtrlC)
		if err != nil {
			log.Fatal(err)
		}
	}
}

func showPorts() error {
	maps, err := getTargetSpecs()
	if err != nil {
		return err
	}

	portsList, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return err
	}
	for _, p := range portsList {
		vid := strings.ToLower(p.VID)
		pid := strings.ToLower(p.PID)
		target := ""
		for k, v := range maps {
			usbInterfaces := v.SerialPort
			for _, s := range usbInterfaces {
				parts := strings.Split(s, ":")
				if len(parts) != 3 || (parts[0] != "acm" && parts[0] == "usb") {
					continue
				}
				if vid == strings.ToLower(parts[1]) && pid == strings.ToLower(parts[2]) {
					target = k
				}
			}
		}
		fmt.Printf("%s %4s %4s %s\n", p.Name, p.VID, p.PID, target)
	}

	return nil
}

func getTargetSpecs() (map[string]TargetSpec, error) {
	out, err := exec.Command(`tinygo`, `env`, `TINYGOROOT`).CombinedOutput()
	if err != nil {
		return nil, err
	}
	root := strings.TrimSpace(string(out))

	matches, err := filepath.Glob(filepath.Join(root, "targets", "*.json"))
	if err != nil {
		return nil, err
	}

	maps := map[string]TargetSpec{}
	for _, match := range matches {
		option := TargetSpec{}
		b, err := ioutil.ReadFile(match)
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(b, &option)
		if err != nil {
			return nil, err
		}
		target := strings.TrimSuffix(filepath.Base(match), ".json")
		maps[target] = option
	}

	return maps, nil
}

func run(p, target string, baud int, disableCtrlC bool) error {
	maps, err := getTargetSpecs()
	if err != nil {
		return err
	}
	usbInterfaces := []string{}
	if target != "" {
		if ifs, ok := maps[target]; ok {
			usbInterfaces = ifs.SerialPort
		}
	}

	p2, err := getDefaultPort(p, usbInterfaces)
	if err != nil {
		return err
	}

	if disableCtrlC {
		fmt.Printf("`Ctrl-x q` to exit\n")
	}

	var mu sync.Mutex

	port, err := serial.Open(p2, &serial.Mode{BaudRate: baud})

	for i := 0; i < 50; i++ {
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
		port, err = serial.Open(p, &serial.Mode{BaudRate: baud})
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
					port, err = serial.Open(p, &serial.Mode{BaudRate: baud})
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

// getDefaultPort returns the default serial port depending on the operating system.
func getDefaultPort(portFlag string, usbInterfaces []string) (port string, err error) {
	portCandidates := strings.FieldsFunc(portFlag, func(c rune) bool { return c == ',' })
	if len(portCandidates) == 1 {
		return portCandidates[0], nil
	}

	var ports []string
	switch runtime.GOOS {
	case "freebsd":
		ports, err = filepath.Glob("/dev/cuaU*")
	case "darwin", "linux", "windows":
		var portsList []*enumerator.PortDetails
		portsList, err = enumerator.GetDetailedPortsList()
		if err != nil {
			return "", err
		}

		var preferredPortIDs [][2]uint16
		for _, s := range usbInterfaces {
			parts := strings.Split(s, ":")
			if len(parts) != 3 || (parts[0] != "acm" && parts[0] == "usb") {
				// acm and usb are the two types of serial ports recognized
				// under Linux (ttyACM*, ttyUSB*). Other operating systems don't
				// generally make this distinction. If this is not one of the
				// given USB devices, don't try to parse the USB IDs.
				continue
			}
			vid, err := strconv.ParseUint(parts[1], 16, 16)
			if err != nil {
				return "", fmt.Errorf("could not parse USB vendor ID %q: %w", parts[1], err)
			}
			pid, err := strconv.ParseUint(parts[2], 16, 16)
			if err != nil {
				return "", fmt.Errorf("could not parse USB product ID %q: %w", parts[1], err)
			}
			preferredPortIDs = append(preferredPortIDs, [2]uint16{uint16(vid), uint16(pid)})
		}

		var primaryPorts []string   // ports picked from preferred USB VID/PID
		var secondaryPorts []string // other ports (as a fallback)
		for _, p := range portsList {
			if !p.IsUSB {
				continue
			}
			if p.VID != "" && p.PID != "" {
				foundPort := false
				vid, vidErr := strconv.ParseUint(p.VID, 16, 16)
				pid, pidErr := strconv.ParseUint(p.PID, 16, 16)
				if vidErr == nil && pidErr == nil {
					for _, id := range preferredPortIDs {
						if uint16(vid) == id[0] && uint16(pid) == id[1] {
							primaryPorts = append(primaryPorts, p.Name)
							foundPort = true
							continue
						}
					}
				}
				if foundPort {
					continue
				}
			}

			secondaryPorts = append(secondaryPorts, p.Name)
		}
		if len(primaryPorts) == 1 {
			// There is exactly one match in the set of preferred ports. Use
			// this port, even if there may be others available. This allows
			// flashing a specific board even if there are multiple available.
			return primaryPorts[0], nil
		} else if len(primaryPorts) > 1 {
			// There are multiple preferred ports, probably because more than
			// one device of the same type are connected (e.g. two Arduino
			// Unos).
			ports = primaryPorts
		} else {
			// No preferred ports found. Fall back to other serial ports
			// available in the system.
			ports = secondaryPorts
		}

		if len(ports) == 0 {
			// fallback
			switch runtime.GOOS {
			case "darwin":
				ports, err = filepath.Glob("/dev/cu.usb*")
			case "linux":
				ports, err = filepath.Glob("/dev/ttyACM*")
			case "windows":
				ports, err = serial.GetPortsList()
			}
		}
	default:
		return "", errors.New("unable to search for a default USB device to be flashed on this OS")
	}

	if err != nil {
		return "", err
	} else if ports == nil {
		return "", errors.New("unable to locate a serial port")
	} else if len(ports) == 0 {
		return "", errors.New("no serial ports available")
	}

	if len(portCandidates) == 0 {
		if len(ports) == 1 {
			return ports[0], nil
		} else {
			return "", errors.New("multiple serial ports available - use -port flag, available ports are " + strings.Join(ports, ", "))
		}
	}

	for _, ps := range portCandidates {
		for _, p := range ports {
			if p == ps {
				return p, nil
			}
		}
	}

	return "", errors.New("port you specified '" + strings.Join(portCandidates, ",") + "' does not exist, available ports are " + strings.Join(ports, ", "))
}

type TargetSpec struct {
	SerialPort []string `json:"serial-port"` // serial port IDs in the form "acm:vid:pid" or "usb:vid:pid"
}
