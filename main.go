package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
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

	// Plato's online flag lags reality (it only flips on an observed
	// network-up event), so always verify against the server itself.
	netUp := watchStdin(stdin)
	if err := client.Ping(ctx); err != nil {
		if !wifi {
			emit.Notify("Turning Wi-Fi on…")
			emit.SetWifi(true)
		} else if !online {
			emit.Notify("Waiting for Wi-Fi to connect…")
		} else {
			emit.Notify("Server unreachable, retrying…")
		}
		if !waitForServer(ctx, client.Ping, netUp, 2*time.Minute, 10*time.Second) {
			if ctx.Err() == nil {
				emit.Notify("Couldn't reach the WebDAV server after 2 minutes. Check that the Kobo is on the right Wi-Fi network and the server is running.")
			}
			return 1
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

	// We never force Wi-Fi off on exit: when Plato hands control back to
	// Nickel, cutting the network mid-handoff can fail Nickel's account
	// sync and drop the device to the activation screen. Leave Wi-Fi as-is
	// and let Plato/Nickel own its lifecycle.
	if err != nil && ctx.Err() == nil {
		return fail(err)
	}
	return 0
}
