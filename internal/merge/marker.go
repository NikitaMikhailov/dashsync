package merge

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/token"
)

// markerPrefix identifies a comment as dashsync's own, distinguishing it
// from any comment a human happened to leave on the same entry.
const markerPrefix = "dashsync:managed"

// marker is the parsed form of a "# dashsync:managed id=... content=..."
// comment: dashsync's only way of recognizing, on a later run, which YAML
// list entries it generated versus which ones a human wrote by hand.
type marker struct {
	id      string // model.Service.ID — identifies *which* service this is
	content string // fingerprint of what dashsync wrote for it last time
}

// buildMarker renders a marker comment for one managed entry.
func buildMarker(id, content string) *ast.CommentGroupNode {
	text := fmt.Sprintf("# %s id=%s content=%s\n", markerPrefix, id, content)

	var toks []*token.Token
	for _, t := range lexer.Tokenize(text) {
		if t.Type == token.CommentType {
			toks = append(toks, t)
		}
	}
	return ast.CommentGroup(toks)
}

// parseMarker reads a marker back out of a comment attached to a sequence
// entry. ok is false for a nil, malformed, or foreign comment — anything
// that isn't unambiguously a marker dashsync itself would have written, so
// a human's own comment on an entry is never mistaken for one and treated
// as unmanaged instead.
func parseMarker(cg *ast.CommentGroupNode) (marker, bool) {
	if cg == nil {
		return marker{}, false
	}

	line := strings.TrimSpace(cg.String())
	line = strings.TrimLeft(line, "#")
	line = strings.TrimSpace(line)

	rest, ok := strings.CutPrefix(line, markerPrefix)
	if !ok {
		return marker{}, false
	}

	var m marker
	for _, field := range strings.Fields(rest) {
		key, value, hasEquals := strings.Cut(field, "=")
		if !hasEquals {
			continue
		}
		switch key {
		case "id":
			m.id = value
		case "content":
			m.content = value
		}
	}
	if m.id == "" || m.content == "" {
		return marker{}, false
	}
	return m, true
}

// contentHash fingerprints rendered entry bytes for hand-edit detection.
// It only needs to resist an accidental match, not a deliberate one — a
// human editing a services.yaml file isn't an adversary — so a fast
// non-cryptographic hash is the right tool, not sha256.
func contentHash(b []byte) string {
	h := fnv.New32a()
	_, _ = h.Write(b) // hash.Hash.Write never returns an error
	return strconv.FormatUint(uint64(h.Sum32()), 16)
}
