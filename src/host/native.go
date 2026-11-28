//go:build !wasip1

package host

import (
	"encoding/json"
	"time"
)

// Native is what `go vet`/`go build` on a developer machine binds: nothing
// is available, so every capability fails closed. Real behaviour is tested
// on src/hosttest; the shipped module is the wasip1 build.
type Native struct{}

// Default is the host main binds to.
func Default() Host { return Native{} }

func (Native) DB() DB           { return unavailableDB{} }
func (Native) Views() Views     { return unavailableViews{} }
func (Native) Records() Records { return unavailableRecords{} }
func (Native) Clock() Clock     { return nativeClock{} }
func (Native) IDs() IDs         { return nativeIDs{} }

type unavailableViews struct{}

func (unavailableViews) Query(string, any) (json.RawMessage, error) { return nil, ErrUnavailable }

type nativeClock struct{}

func (nativeClock) Now() time.Time { return time.Now().UTC() }

type nativeIDs struct{}

func (nativeIDs) NewID() string { return "" }
