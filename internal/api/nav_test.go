package api

import (
	"strings"
	"testing"

	"github.com/kubecron/kubecron/internal/storage"
)

// The nav's cluster control adapts to how many clusters exist: a picker is only
// a control when there is something to pick.
func TestClusterNav(t *testing.T) {
	one := []storage.Cluster{{ID: "prod", Name: "prod"}}
	two := []storage.Cluster{{ID: "prod", Name: "prod"}, {ID: "staging", Name: "staging"}}

	t.Run("no clusters renders nothing", func(t *testing.T) {
		if got := clusterNav(navState{}); got != "" {
			t.Errorf("clusterNav() = %q, want empty", got)
		}
	})

	t.Run("one cluster renders a label, not a dropdown", func(t *testing.T) {
		got := clusterNav(navState{Clusters: one})
		if strings.Contains(got, "<select") {
			t.Error("single cluster rendered a <select>; a one-option picker cannot do anything")
		}
		if !strings.Contains(got, `href="/clusters/prod"`) {
			t.Errorf("clusterNav() = %q, want a link to the cluster", got)
		}
		if !strings.Contains(got, ">prod<") {
			t.Errorf("clusterNav() = %q, want the cluster named", got)
		}
	})

	t.Run("several clusters render a picker with All clusters", func(t *testing.T) {
		got := clusterNav(navState{Clusters: two})
		if !strings.Contains(got, "<select") {
			t.Error("multiple clusters did not render a <select>")
		}
		for _, want := range []string{`value="/"`, `value="/clusters/prod"`, `value="/clusters/staging"`, "All clusters"} {
			if !strings.Contains(got, want) {
				t.Errorf("clusterNav() missing %q\ngot: %s", want, got)
			}
		}
	})

	t.Run("active cluster is preselected", func(t *testing.T) {
		got := clusterNav(navState{Clusters: two, ActiveCluster: "staging"})
		if !strings.Contains(got, `value="/clusters/staging" selected`) {
			t.Errorf("active cluster not selected\ngot: %s", got)
		}
		if strings.Contains(got, `value="/" selected`) {
			t.Error(`"All clusters" selected while a cluster is active`)
		}
	})

	t.Run("overview preselects All clusters", func(t *testing.T) {
		got := clusterNav(navState{Clusters: two})
		if !strings.Contains(got, `value="/" selected`) {
			t.Errorf("overview did not preselect All clusters\ngot: %s", got)
		}
	})

	// Cluster IDs come from kubeconfig filenames, which are operator-supplied
	// and must not be able to inject markup into the nav.
	t.Run("cluster names are escaped", func(t *testing.T) {
		evil := []storage.Cluster{
			{ID: `a"><script>alert(1)</script>`, Name: `a"><script>alert(1)</script>`},
			{ID: "b", Name: "b"},
		}
		got := clusterNav(navState{Clusters: evil})
		if strings.Contains(got, "<script>") {
			t.Errorf("unescaped markup reached the nav: %s", got)
		}
	})
}

// normalizeWindow guards the range switch: an out-of-range or malformed window
// must fall back rather than render a page whose heading disagrees with its data.
func TestNormalizeWindow(t *testing.T) {
	cases := map[int]int{1: 1, 7: 7, 30: 30, 0: 7, -5: 7, 999: 7, 8: 7}
	for in, want := range cases {
		if got := normalizeWindow(in); got != want {
			t.Errorf("normalizeWindow(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestAtoiDefault(t *testing.T) {
	cases := []struct {
		in   string
		def  int
		want int
	}{
		{"30", 7, 30},
		{"", 7, 7},
		{"abc", 7, 7},
		{"1e3", 7, 7},
		{"-1", 7, -1},
	}
	for _, tc := range cases {
		if got := atoiDefault(tc.in, tc.def); got != tc.want {
			t.Errorf("atoiDefault(%q, %d) = %d, want %d", tc.in, tc.def, got, tc.want)
		}
	}
}

func TestFmtBytes(t *testing.T) {
	cases := map[int64]string{
		512:        "512 B",
		2048:       "2 KiB",
		5 << 20:    "5 MiB",
		3 << 30:    "3.00 GiB",
		1536 << 20: "1.50 GiB",
		0:          "0 B",
	}
	for in, want := range cases {
		if got := fmtBytes(in); got != want {
			t.Errorf("fmtBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFmtMillicores(t *testing.T) {
	cases := map[int64]string{
		1:    "1m",
		999:  "999m",
		1000: "1.00 cores",
		2500: "2.50 cores",
	}
	for in, want := range cases {
		if got := fmtMillicores(in); got != want {
			t.Errorf("fmtMillicores(%d) = %q, want %q", in, got, want)
		}
	}
}
