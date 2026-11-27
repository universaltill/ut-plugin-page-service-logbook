// Service Staff Logbook (com.universaltill.service-logbook): per-staff
// service, tip and takings logbook for service shops (ADR-0149), a WASI
// module the till runs in-process (ADR-0001, ADR-0121). Build with
// GOOS=wasip1 GOARCH=wasm (scripts/build.sh). The logic lives in src/store
// and src/app, unit-tested natively on an in-memory host (src/hosttest).
//
// The plugin cannot run until #3157 slice B (plugin SQLite host functions)
// and the #3697 records channel exist; src/host/wasm.go fails closed until then.
package main

import (
	"github.com/universaltill/universal-till/sdk/plugin"
	"github.com/universaltill/ut-plugin-page-service-logbook/src/app"
	"github.com/universaltill/ut-plugin-page-service-logbook/src/host"
)

func main() {
	plugin.Run(app.Handlers(host.Default()))
}
