// Public Domain (-) 2016 The GitFund Authors.
// See the GitFund UNLICENSE file for details.

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsevents"
	"github.com/tav/golly/log"
	"github.com/tav/golly/optparse"
	"github.com/tav/golly/process"
)

var running *exec.Cmd

var (
	svcDir = ""
	svcPat = ""
	kill   = &sync.Mutex{}
	killed = false
	mutex  = &sync.Mutex{}
	wg     = &sync.WaitGroup{}
)

func killProcess() {
	mutex.Lock()
	if running != nil {
		kill.Lock()
		killed = true
		kill.Unlock()
		pgid, err := syscall.Getpgid(running.Process.Pid)
		if err != nil {
			fmt.Printf("ERROR: failed to get the pgid for the 'go run' process: %s\n", err)
			os.Exit(1)
		}
		syscall.Kill(-pgid, 15)
		if err != nil {
			fmt.Printf("ERROR: failed to kill the 'go run' process: %s\n", err)
			os.Exit(1)
		}
		mutex.Unlock()
		wg.Wait()
		kill.Lock()
		killed = false
		kill.Unlock()
	} else {
		mutex.Unlock()
	}
}

func run() {
	killProcess()
	fmt.Println("\n------------------------- BUILDING AND RUNNING SERVICE -------------------------\n")
	matches, err := filepath.Glob(svcPat)
	if err != nil {
		log.Fatal(err)
	}
	args := append([]string{"run"}, matches...)
	cmd := exec.Command("go", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		fmt.Printf("\nERROR: %s\n", err)
		return
	}
	running = cmd
	wg.Add(1)
	go func() {
		err := cmd.Wait()
		if err != nil {
			kill.Lock()
			if !killed {
				fmt.Printf("\n!! %s\n", err)
			}
			kill.Unlock()
		}
		mutex.Lock()
		running = nil
		mutex.Unlock()
		wg.Done()
	}()
}

func main() {

	opts := optparse.New("Usage: run [OPTIONS] SERVICE_DIR\n")

	watch := opts.Flags("-w", "--watch").Label("PATHS").String(
		"Comma-delimited list of additional paths to watch")

	os.Args[0] = "run"
	args := opts.Parse(os.Args)
	if len(args) != 1 {
		opts.PrintUsage()
		process.Exit(1)
	}

	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	svcDir = filepath.Join(cwd, args[0])
	svcPat = filepath.Join(svcDir, "*.go")
	paths := []string{svcDir}
	for _, path := range strings.Split(*watch, ",") {
		paths = append(paths, filepath.Join(cwd, path))
	}

	if os.Getenv("DATASTORE_EMULATOR_HOST") == "" {
		os.Setenv("DATASTORE_EMULATOR_HOST", "localhost:8801")
	}

	if os.Getenv("PUBSUB_EMULATOR_HOST") == "" {
		os.Setenv("PUBSUB_EMULATOR_HOST", "localhost:8802")
	}

	watcher := &fsevents.EventStream{
		Paths:   paths,
		Latency: 50 * time.Millisecond,
		Flags:   fsevents.FileEvents | fsevents.WatchRoot,
	}

	watcher.Start()
	process.SetExitHandler(killProcess)
	for {
		run()
		<-watcher.Events
	}

}
