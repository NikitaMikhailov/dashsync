package discovery

import (
	"strconv"
	"strings"

	"github.com/NikitaMikhailov/dashsync/internal/model"
)

// Port is dashsync's minimal view of a container's published port mapping —
// just enough for URL auto-detection, without pulling the Docker API's own
// port type into this file's otherwise Docker-free logic.
type Port struct {
	// Public is the port exposed on the host. A port that isn't published
	// (container-only) has no meaningful value here and is filtered out
	// before it reaches ParseLabels — see toPorts in docker.go.
	Public uint16
	Type   string // "tcp", "udp", or "sctp"
}

// ParseLabels turns one container's name and Docker labels into a
// model.Service. ok is false if the container never opted in.
//
// Label contract:
//
//	dashsync.enable        "true" to opt in. Nothing else is read unless
//	                        this is set — opt-in, not opt-out, so a
//	                        container never ends up on a dashboard by
//	                        accident just because it happens to expose a
//	                        port.
//	dashsync.name           display name (default: the container's own
//	                        name)
//	dashsync.group          dashboard group (default: "Other")
//	dashsync.url            explicit URL (default: derived from the
//	                        lowest-numbered published tcp port, as
//	                        http://hostAddr:port)
//	dashsync.icon           icon identifier, passed through as-is
//	dashsync.description    short description
//	dashsync.<anything else>  collected into Service.Extra, keyed by
//	                        everything after "dashsync." — e.g.
//	                        "dashsync.homepage.widget.type" becomes the
//	                        Extra key "homepage.widget.type", for a
//	                        renderer to interpret on its own later.
func ParseLabels(name, host, status string, labels map[string]string, ports []Port, hostAddr string) (model.Service, bool) {
	if labels["dashsync.enable"] != "true" {
		return model.Service{}, false
	}

	source := model.Source{Host: host, Container: name}
	svc := model.Service{
		ID:          source.ID(),
		Name:        firstNonEmpty(labels["dashsync.name"], name),
		Group:       firstNonEmpty(labels["dashsync.group"], "Other"),
		URL:         firstNonEmpty(labels["dashsync.url"], urlFromPorts(ports, hostAddr)),
		Icon:        labels["dashsync.icon"],
		Status:      status,
		Description: labels["dashsync.description"],
		Source:      source,
	}

	for key, value := range labels {
		suffix, isDashsyncLabel := strings.CutPrefix(key, "dashsync.")
		if !isDashsyncLabel || suffix == "" || isKnownLabelKey(suffix) {
			continue
		}
		if svc.Extra == nil {
			svc.Extra = make(map[string]string)
		}
		svc.Extra[suffix] = value
	}

	return svc, true
}

func isKnownLabelKey(suffix string) bool {
	switch suffix {
	case "enable", "name", "group", "url", "icon", "description":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// urlFromPorts guesses a service's URL from its published tcp ports,
// picking the lowest-numbered one so the result doesn't depend on the
// order the Docker API happened to return them in — that order isn't
// documented as stable, and dashsync's idempotency guarantee can't rest on
// an assumption the API never promised. It returns "" (not an error) when
// there's nothing to guess from — plenty of dashsync-managed services
// (background jobs, agents) have no web UI at all, and that's not a
// problem to report.
func urlFromPorts(ports []Port, hostAddr string) string {
	var lowest uint16
	for _, p := range ports {
		if p.Type != "tcp" || p.Public == 0 {
			continue
		}
		if lowest == 0 || p.Public < lowest {
			lowest = p.Public
		}
	}
	if lowest == 0 {
		return ""
	}
	return "http://" + hostAddr + ":" + strconv.Itoa(int(lowest))
}
