package main

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	args := os.Args[1:]

	// If RECORD_FILE is set in env, record that this invocation happened.
	if recordFile := os.Getenv("RECORD_FILE"); recordFile != "" {
		f, err := os.OpenFile(recordFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(f, "invoked: %s\n", strings.Join(args, " "))
			f.Close()
		}
	}

	if len(args) == 0 {
		fmt.Println("fakecli: no arguments provided")
		return
	}

	switch args[0] {
	case "--version", "-v", "-V", "-version", "version":
		fmt.Println("fakecli version 2.4.0 (x86_64-fake-platform)")
	case "--help", "-h", "help":
		fmt.Println("fakecli: a helper binary for testing cliexec")
	case "exit":
		code := 1
		if len(args) > 1 {
			if parsed, err := strconv.Atoi(args[1]); err == nil {
				code = parsed
			}
		}
		if len(args) > 2 {
			fmt.Fprintln(os.Stderr, args[2])
		}
		os.Exit(code)
	case "sleep":
		duration := 2 * time.Second
		if len(args) > 1 {
			if parsed, err := time.ParseDuration(args[1]); err == nil {
				duration = parsed
			}
		}
		time.Sleep(duration)
	case "dump-env":
		for _, kv := range os.Environ() {
			fmt.Println(kv)
		}
	case "oversized":
		// Print 2MB of stdout and 2MB of stderr
		chunk := bytes.Repeat([]byte("stdout-filler-chunk-1234567890\n"), 1000)
		for i := 0; i < 70; i++ {
			os.Stdout.Write(chunk)
		}
		errChunk := bytes.Repeat([]byte("stderr-filler-chunk-0987654321\n"), 1000)
		for i := 0; i < 70; i++ {
			os.Stderr.Write(errChunk)
		}
	default:
		fmt.Printf("fakecli: unknown command %s\n", args[0])
	}
}
