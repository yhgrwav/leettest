// cpu runs a command and prints its CPU time as a share of its wall time.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

func main() {
	cmd := exec.Command(os.Args[1], os.Args[2:]...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	start := time.Now()
	err := cmd.Run()
	wall := time.Since(start)
	st := cmd.ProcessState
	cpu := st.UserTime() + st.SystemTime()
	fmt.Fprintf(os.Stderr, "CPU %.0f%% of a core (%v over %v), exit %v\n",
		100*float64(cpu)/float64(wall), cpu.Round(time.Millisecond), wall.Round(time.Millisecond), err)
}
