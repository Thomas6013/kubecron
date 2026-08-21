package api

import "fmt"

// Mode selects which half of KubeCron's HTTP surface is registered.
//
// The two modes are the same assembly — storage, cluster registry, sampler,
// streamer and the informer controllers all run identically. Only the routes
// differ, which is why this is a flag and not a build tag: a collector that
// recorded a different set of facts from the standalone product would be a
// second product to keep correct.
type Mode string

const (
	// ModeUI is the standalone product: the server-rendered dashboard, the JSON
	// API that backs it, and the CronJob controls (suspend/resume/trigger).
	// This is the default and the historical behaviour.
	ModeUI Mode = "ui"

	// ModeServer is headless collector mode: the versioned read-only API under
	// /api/v1 and nothing else. No HTML, no static assets, no mutating routes.
	//
	// It exists so KubeCron can be deployed one-per-cluster as the recorder
	// KubeDeck is not (KubeDeck is a fat client and cannot watch a cluster it
	// is not open against), and read on demand by a console that was not
	// running when a run happened.
	ModeServer Mode = "server"
)

// ParseMode resolves the KUBECRON_MODE value. An empty value means ModeUI so
// that an existing deployment that sets nothing keeps the behaviour it has.
//
// "standalone" and "collector" are accepted as aliases because they are what
// the two modes are called in prose, and an operator who types the word from
// the documentation should not get an error.
func ParseMode(s string) (Mode, error) {
	switch s {
	case "", "ui", "standalone":
		return ModeUI, nil
	case "server", "collector":
		return ModeServer, nil
	default:
		return "", fmt.Errorf("api: unknown mode %q (want \"ui\" or \"server\")", s)
	}
}

// ServesUI reports whether the HTML dashboard and its static assets are served.
func (m Mode) ServesUI() bool { return m == ModeUI }

// AllowsMutation reports whether the suspend/resume/trigger routes are
// registered.
//
// Collector mode is strictly read-only, and that is a design decision rather
// than a simplification: KubeDeck performs those actions itself, through its
// own confirmation panels and its own per-cluster read-only guardrail. Two
// components able to mutate the same CronJob would be two authorization models
// that have to stay in agreement — and KubeCron's is off by default.
func (m Mode) AllowsMutation() bool { return m == ModeUI }
