// capture-child is a local supervisor fixture, never a lab workload binary.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	var work string
	for i, arg := range os.Args {
		if arg == "--data-root" && i+1 < len(os.Args) {
			work = filepath.Dir(os.Args[i+1])
		}
	}
	if work == "" {
		panic("missing owned work root")
	}
	terms := make(chan os.Signal, 4)
	signal.Notify(terms, syscall.SIGTERM)
	go func() {
		for count := 1; ; count++ {
			<-terms
			if err := os.WriteFile(filepath.Join(work, "terms"), []byte(fmt.Sprint(count)), 0o600); err != nil {
				panic(err)
			}
			if count > 1 {
				os.Exit(0)
			}
		}
	}()
	// Read the receipt after the real supervisor has returned from exec.
	time.Sleep(time.Second)
	raw, err := os.ReadFile(filepath.Join(work, "evidence", "root-hfs-direct.process.json"))
	if err != nil {
		panic(err)
	}
	var receipt struct {
		Guards []int `json:"retained_pipe_read_fds"`
	}
	if err := json.Unmarshal(raw, &receipt); err != nil || len(receipt.Guards) != 2 {
		panic("missing pipe guards")
	}
	for _, fd := range receipt.Guards {
		var st syscall.Stat_t
		if err := syscall.Fstat(fd, &st); err != nil || st.Mode&syscall.S_IFMT != syscall.S_IFIFO {
			panic("guard is not an inherited pipe")
		}
		flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_GETFD, 0)
		if errno != 0 || flags&syscall.FD_CLOEXEC != 0 {
			panic("pipe guard did not survive exec")
		}
	}
	if _, err := os.Stdout.Write([]byte("late Go stdout")); err != nil {
		panic(err)
	}
	if _, err := os.Stderr.Write([]byte("late Go stderr")); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(work, "survived"), []byte("yes"), 0o600); err != nil {
		panic(err)
	}
	for {
		time.Sleep(time.Second)
	}
}
