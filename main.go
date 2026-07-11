package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout))
}

func run(args []string, stdin io.Reader, stdout io.Writer) int {
	emit := NewEmitter(stdout)
	if len(args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: plato-webdav LIBRARY_PATH SAVE_PATH WIFI ONLINE")
		return 1
	}
	library, saveDir := args[0], args[1]
	wifi, online := args[2] == "true", args[3] == "true"

	fail := func(err error) int {
		emit.Notify(fmt.Sprintf("WebDAV sync: %v", err))
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	cfg, err := loadConfig("Settings.json")
	if err != nil {
		return fail(err)
	}
	client, err := NewClient(cfg)
	if err != nil {
		return fail(err)
	}
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		return fail(err)
	}

	// Plato terminates fetchers with SIGTERM; finish the current file and
	// exit cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	netUp := watchStdin(stdin)
	wifiWasOff := false
	if !online {
		if !wifi {
			wifiWasOff = true
			emit.Notify("Establishing a network connection.")
			emit.SetWifi(true)
		} else {
			emit.Notify("Waiting for the network to come up.")
		}
		if !waitForNetwork(ctx, netUp) {
			return 0
		}
	}

	syncer := &Syncer{
		cfg:     cfg,
		client:  client,
		emit:    emit,
		state:   loadState(saveDir),
		library: library,
		saveDir: saveDir,
	}
	err = syncer.Run(ctx)

	if wifiWasOff {
		emit.SetWifi(false)
	}
	if err != nil && ctx.Err() == nil {
		return fail(err)
	}
	return 0
}
