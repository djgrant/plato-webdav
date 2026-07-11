# plato-webdav

A [Plato](https://github.com/baskerville/plato) fetcher hook that keeps a
library folder in sync with a WebDAV server (Nextcloud, ownCloud, nginx,
Apache mod_dav, `rclone serve webdav`, …).

When you open the hooked folder on your Kobo, Plato launches this binary. It
lists the server, downloads new books, removes local books that were deleted
on the server, reports progress as notifications, and exits. Books you
sideload into the folder yourself are never touched.

## Install

1. Build the Kobo binary:

   ```sh
   make kobo    # → dist/plato-webdav (static ARMv7 Linux ELF)
   ```

2. Copy it and a config to the device:

   ```
   /mnt/onboard/.adds/plato-webdav/plato-webdav
   /mnt/onboard/.adds/plato-webdav/Settings.json   (start from Settings.sample.json)
   ```

3. Add a hook to Plato's `Settings.toml`:

   ```toml
   [[libraries.hooks]]
   path = "WebDAV"
   program = "/mnt/onboard/.adds/plato-webdav/plato-webdav"
   sort-method = "added"
   second-column = "progress"
   ```

Tapping the `WebDAV` directory in Plato's navigation bar triggers the sync.
If the device is offline, the hook turns on Wi-Fi, waits for the network,
syncs, and turns Wi-Fi back off.

## Configuration (`Settings.json`, next to the binary)

| Key | Default | Meaning |
| --- | --- | --- |
| `server-url` | *(required)* | WebDAV collection URL, e.g. `https://cloud.example.com/remote.php/dav/files/user/` |
| `path` | `""` | Path appended to `server-url` (the folder to sync) |
| `username` / `password` | `""` | HTTP Basic auth credentials |
| `insecure-skip-verify` | `false` | Skip TLS certificate verification (self-signed servers) |
| `recursive` | `true` | Mirror server subdirectories into the folder |
| `delete-removed` | `true` | Delete local copies of books removed on the server |
| `allowed-kinds` | Plato's defaults | File extensions to sync |
| `timeout-seconds` | `60` | HTTP response-header timeout |

Change detection uses ETags (falling back to size + last-modified), stored in
`.sync-state.json` inside the synced folder. Downloads go to hidden
`.partial` files and are renamed into place only when complete, so
interrupted syncs never leave broken books visible to Plato.

## Development

```sh
make test    # go vet + unit tests (fake WebDAV server via httptest)
make build   # host binary
```

Simulate a Plato invocation locally (argv: library, save dir, wifi, online):

```sh
printf '{"type":"network","status":"up"}\n' | \
  ./plato-webdav /tmp/lib /tmp/lib/WebDAV false false
```

The hook speaks Plato's line-delimited JSON protocol on stdout
(`notify`, `addDocument`, `removeDocument`, `setWifi`) and listens for
network events on stdin; Plato stops it with SIGTERM, which finishes the
current download and exits cleanly.

Basic auth only for now; Digest auth would be a straightforward follow-up.
