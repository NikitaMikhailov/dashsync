package discovery

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/NikitaMikhailov/dashsync/internal/model"
)

func TestParseLabels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ctrName  string
		host     string
		status   string
		labels   map[string]string
		ports    []Port
		hostAddr string
		wantOK   bool
		want     model.Service
	}{
		{
			name:    "not enabled is skipped entirely",
			ctrName: "jellyfin",
			host:    "homelab-1",
			labels:  map[string]string{"dashsync.name": "Jellyfin"},
			wantOK:  false,
		},
		{
			name:    "enable=false is still not enabled",
			ctrName: "jellyfin",
			host:    "homelab-1",
			labels:  map[string]string{"dashsync.enable": "false"},
			wantOK:  false,
		},
		{
			name:    "minimal enabled container falls back to defaults",
			ctrName: "jellyfin",
			host:    "homelab-1",
			status:  "running",
			labels:  map[string]string{"dashsync.enable": "true"},
			wantOK:  true,
			want: model.Service{
				ID:     model.Source{Host: "homelab-1", Container: "jellyfin"}.ID(),
				Name:   "jellyfin",
				Group:  "Other",
				Status: "running",
				Source: model.Source{Host: "homelab-1", Container: "jellyfin"},
			},
		},
		{
			name:    "a stopped-but-enabled container keeps its status, not hidden",
			ctrName: "jellyfin",
			host:    "homelab-1",
			status:  "exited",
			labels:  map[string]string{"dashsync.enable": "true"},
			wantOK:  true,
			want: model.Service{
				ID:     model.Source{Host: "homelab-1", Container: "jellyfin"}.ID(),
				Name:   "jellyfin",
				Group:  "Other",
				Status: "exited",
				Source: model.Source{Host: "homelab-1", Container: "jellyfin"},
			},
		},
		{
			name:    "explicit fields override every default",
			ctrName: "jellyfin",
			host:    "homelab-1",
			status:  "running",
			labels: map[string]string{
				"dashsync.enable":      "true",
				"dashsync.name":        "Jellyfin",
				"dashsync.group":       "Media",
				"dashsync.url":         "https://jellyfin.example.com",
				"dashsync.icon":        "jellyfin",
				"dashsync.description": "Media server",
			},
			ports:    []Port{{Public: 8096, Type: "tcp"}},
			hostAddr: "10.0.0.5",
			wantOK:   true,
			want: model.Service{
				ID:          model.Source{Host: "homelab-1", Container: "jellyfin"}.ID(),
				Name:        "Jellyfin",
				Group:       "Media",
				URL:         "https://jellyfin.example.com",
				Icon:        "jellyfin",
				Status:      "running",
				Description: "Media server",
				Source:      model.Source{Host: "homelab-1", Container: "jellyfin"},
			},
		},
		{
			name:     "URL is auto-detected from the published tcp port",
			ctrName:  "jellyfin",
			host:     "homelab-1",
			status:   "running",
			labels:   map[string]string{"dashsync.enable": "true"},
			ports:    []Port{{Public: 8096, Type: "tcp"}},
			hostAddr: "10.0.0.5",
			wantOK:   true,
			want: model.Service{
				ID:     model.Source{Host: "homelab-1", Container: "jellyfin"}.ID(),
				Name:   "jellyfin",
				Group:  "Other",
				URL:    "http://10.0.0.5:8096",
				Status: "running",
				Source: model.Source{Host: "homelab-1", Container: "jellyfin"},
			},
		},
		{
			// A container can publish more than one tcp port; the pick must
			// not depend on which order the Docker API happened to list
			// them in, or the auto-detected URL could flip between runs
			// with no label change — a direct hit on the idempotency
			// guarantee.
			name:     "with multiple tcp ports, the lowest-numbered one wins regardless of order",
			ctrName:  "app",
			host:     "homelab-1",
			labels:   map[string]string{"dashsync.enable": "true"},
			ports:    []Port{{Public: 9090, Type: "tcp"}, {Public: 8080, Type: "tcp"}, {Public: 8443, Type: "tcp"}},
			hostAddr: "10.0.0.5",
			wantOK:   true,
			want: model.Service{
				ID:     model.Source{Host: "homelab-1", Container: "app"}.ID(),
				Name:   "app",
				Group:  "Other",
				URL:    "http://10.0.0.5:8080",
				Source: model.Source{Host: "homelab-1", Container: "app"},
			},
		},
		{
			name:    "udp-only ports are not linkable, URL stays empty",
			ctrName: "pihole",
			host:    "homelab-1",
			labels:  map[string]string{"dashsync.enable": "true"},
			ports:   []Port{{Public: 53, Type: "udp"}},
			wantOK:  true,
			want: model.Service{
				ID:     model.Source{Host: "homelab-1", Container: "pihole"}.ID(),
				Name:   "pihole",
				Group:  "Other",
				Source: model.Source{Host: "homelab-1", Container: "pihole"},
			},
		},
		{
			name:    "unpublished ports (Public=0) are ignored for URL detection",
			ctrName: "internal-svc",
			host:    "homelab-1",
			labels:  map[string]string{"dashsync.enable": "true"},
			ports:   []Port{{Public: 0, Type: "tcp"}},
			wantOK:  true,
			want: model.Service{
				ID:     model.Source{Host: "homelab-1", Container: "internal-svc"}.ID(),
				Name:   "internal-svc",
				Group:  "Other",
				Source: model.Source{Host: "homelab-1", Container: "internal-svc"},
			},
		},
		{
			name:    "unknown dashsync labels are collected into Extra",
			ctrName: "jellyfin",
			host:    "homelab-1",
			labels: map[string]string{
				"dashsync.enable":               "true",
				"dashsync.homepage.widget.type": "jellyfin",
				"dashsync.homepage.widget.key":  "abc123",
				"some.unrelated.label":          "ignored",
			},
			wantOK: true,
			want: model.Service{
				ID:    model.Source{Host: "homelab-1", Container: "jellyfin"}.ID(),
				Name:  "jellyfin",
				Group: "Other",
				Extra: map[string]string{
					"homepage.widget.type": "jellyfin",
					"homepage.widget.key":  "abc123",
				},
				Source: model.Source{Host: "homelab-1", Container: "jellyfin"},
			},
		},
		{
			name:    `the literal label "dashsync." is not a valid extra key`,
			ctrName: "jellyfin",
			host:    "homelab-1",
			labels: map[string]string{
				"dashsync.enable": "true",
				"dashsync.":       "weird",
			},
			wantOK: true,
			want: model.Service{
				ID:     model.Source{Host: "homelab-1", Container: "jellyfin"}.ID(),
				Name:   "jellyfin",
				Group:  "Other",
				Source: model.Source{Host: "homelab-1", Container: "jellyfin"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := ParseLabels(tt.ctrName, tt.host, tt.status, tt.labels, tt.ports, tt.hostAddr)
			if ok != tt.wantOK {
				t.Fatalf("ParseLabels() ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseLabels() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
