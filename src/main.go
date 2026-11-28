// Service Staff Logbook (com.universaltill.service-logbook): per-staff
// service, tip and takings logbook for service shops (ADR-0149), a WASI
// module the till runs in-process (ADR-0001, ADR-0121). Build with
// GOOS=wasip1 GOARCH=wasm (scripts/build.sh). The logic lives in src/store
// and src/app, unit-tested natively on an in-memory host (src/hosttest).
//
// src/host/wasm.go binds the real till (ABI 2): plugin SQLite, read views and
// the plugin-records channel. scripts/realhost-test.sh runs the built module
// against the real host.
package main

import (
	"github.com/universaltill/universal-till/sdk/plugin"
	"github.com/universaltill/ut-plugin-page-service-logbook/src/app"
	"github.com/universaltill/ut-plugin-page-service-logbook/src/host"
)

func main() {
	plugin.Run(app.Handlers(host.Default()))
}
