// Package merge implements dashsync's idempotent sync: given the current
// bytes of a dashboard config file (possibly nonexistent) and the set of
// services dashsync's discovery found, it produces an updated file that
// adds new services, updates ones whose desired content changed, removes
// ones that disappeared, and — the entire reason this isn't just
// unmarshal-modify-marshal — leaves every hand-written entry, comment, and
// ordering choice in the existing file completely untouched.
//
// It works by walking the existing file as a YAML AST (github.com/goccy/go-yaml/ast)
// rather than round-tripping it through Go structs: the latter would throw
// away comments and key order, which is exactly the information a human's
// manual edits live in.
package merge

import (
	"bytes"
	"fmt"

	"github.com/goccy/go-yaml/ast"

	"github.com/NikitaMikhailov/dashsync/internal/model"
)

// ConflictPolicy decides what happens when a managed entry's content in
// the file no longer matches what dashsync wrote for it last time — i.e.,
// a human edited it since.
type ConflictPolicy int

const (
	// Preserve leaves a hand-edited managed entry exactly as it is in the
	// file, permanently (until its service disappears from Docker, or a
	// human removes the marker comment themselves) — the default, because
	// silently overwriting something a human deliberately changed is a
	// worse failure mode than dashsync falling one field behind on it.
	Preserve ConflictPolicy = iota
	// Overwrite replaces a hand-edited managed entry with freshly
	// rendered content anyway.
	Overwrite
	// Fail aborts the entire merge with a *ConflictError the moment one
	// hand-edited managed entry is found, leaving the existing file
	// completely untouched — for a human to look at and decide what to
	// do, rather than dashsync guessing.
	Fail
)

// Options configures Merge.
type Options struct {
	Conflict ConflictPolicy
}

// ChangeKind classifies one entry-level change Merge made, or — for
// Conflict — deliberately didn't.
type ChangeKind int

// The ChangeKind values, in the order Merge processes them for a given
// entry: a service is either Added, Updated, or found in Conflict; a
// service no longer desired is Removed.
const (
	Added ChangeKind = iota
	Updated
	Removed
	Conflict
)

// String implements fmt.Stringer, mainly so internal/diff can format a
// Change without its own switch statement over ChangeKind.
func (k ChangeKind) String() string {
	switch k {
	case Added:
		return "added"
	case Updated:
		return "updated"
	case Removed:
		return "removed"
	case Conflict:
		return "conflict"
	default:
		return "unknown"
	}
}

// Change describes one entry Merge added, updated, removed, or found in
// conflict (left alone under Preserve). ServiceName is empty for Removed —
// the service is gone from the desired set, and Merge only has its old ID
// (from the entry's marker) to report, not a display name.
type Change struct {
	Kind        ChangeKind
	ServiceID   string
	ServiceName string
	Group       string
}

// EntryRenderer is the capability Merge needs beyond model.Group: a way to
// render a single service as a standalone entry, and a way to locate that
// entry within its format's own document shape.
type EntryRenderer interface {
	// RenderEntry renders one service as a standalone entry — the
	// granularity managed markers and hand-edit detection operate on, as
	// opposed to render.Renderer.Render's whole document at a time.
	RenderEntry(s model.Service) ([]byte, error)
	// NormalizeEntry re-renders an existing entry node — as read back from
	// a file — through the same canonical path RenderEntry uses. A node's
	// own String() reflects the column it happened to be parsed at, which
	// varies with how deeply it's nested in the document, so comparing it
	// directly against RenderEntry's output would produce false hand-edit
	// positives on an entry nobody touched.
	NormalizeEntry(node ast.Node) ([]byte, error)
	// Adapter returns the DocumentAdapter that knows this format's
	// document shape — how to locate the list of groups, and a group's own
	// list of entries, within it. The pairing is intrinsic to the format,
	// not a caller choice, which is why the renderer itself owns it rather
	// than Merge taking a separate DocumentAdapter parameter. Merge calls
	// this exactly once per Merge call, so it must be cheap, side-effect
	// free, and safe to call repeatedly across different calls — not
	// something that accumulates state or does meaningful work per call.
	Adapter() DocumentAdapter
}

// ConflictError is returned by Merge under ConflictPolicy Fail when a
// hand-edited managed entry is found.
type ConflictError struct {
	ServiceID   string
	ServiceName string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf(
		"service %q (id %s) was hand-edited since dashsync last wrote it; refusing to continue (conflict policy: fail)",
		e.ServiceName, e.ServiceID)
}

// Merge combines groups — freshly discovered services, already grouped
// and sorted by model.GroupServices — into existing, the current bytes of
// a dashboard config file (nil or empty for "no file yet"), using
// renderer to produce each entry's YAML. It returns the merged document
// and a list of what changed; existing is never modified.
//
// A new group (one with no entry in the existing file at all) is appended
// at the end of the document, in the order it appears in groups — new
// groups don't get inserted alphabetically among existing ones, so a
// hand-reordered file's group order is never fought. Within an existing
// group, new services are likewise appended after whatever's already
// there.
func Merge(existing []byte, groups []model.Group, renderer EntryRenderer, opts Options) ([]byte, []Change, error) {
	file, err := parseOrEmpty(existing)
	if err != nil {
		return nil, nil, fmt.Errorf("parse existing config: %w", err)
	}
	adapter := renderer.Adapter()
	root, err := adapter.GroupSequence(file)
	if err != nil {
		return nil, nil, err
	}

	// desiredGroupOf maps a service ID to the group it belongs to this
	// run — the removal pass below uses it to tell "gone entirely" and
	// "moved to a different group" apart from "still exactly where it
	// was."
	desiredGroupOf := make(map[string]string)
	for _, g := range groups {
		for _, s := range g.Services {
			desiredGroupOf[s.ID] = g.Name
		}
	}

	var changes []Change

	for _, g := range groups {
		groupSeq, err := adapter.FindOrCreateGroup(root, g.Name)
		if err != nil {
			return nil, nil, err
		}

		entries := readEntries(groupSeq)
		entries, groupChanges, err := applyDesired(entries, g.Name, g.Services, renderer, opts)
		if err != nil {
			return nil, nil, err
		}
		changes = append(changes, groupChanges...)

		if err := writeEntries(groupSeq, entries); err != nil {
			return nil, nil, fmt.Errorf("write group %q: %w", g.Name, err)
		}
	}

	// Removal pass: every group currently in the file — including ones
	// with zero desired services left this run — loses any managed entry
	// whose service ID isn't desired for that exact group anymore.
	for _, ng := range adapter.Groups(root) {
		entries := readEntries(ng.Items)
		entries, removedChanges := removeOrphaned(entries, ng.Name, desiredGroupOf)
		if len(removedChanges) == 0 {
			continue
		}
		changes = append(changes, removedChanges...)
		if err := writeEntries(ng.Items, entries); err != nil {
			return nil, nil, fmt.Errorf("write group %q: %w", ng.Name, err)
		}
	}

	return []byte(file.String()), changes, nil
}

// applyDesired adds or updates each of services within a single group's
// entries, per opts.Conflict for any that were hand-edited since dashsync
// last wrote them. It returns the group's new entry list.
func applyDesired(
	entries []seqEntry, groupName string, services []model.Service, renderer EntryRenderer, opts Options,
) ([]seqEntry, []Change, error) {
	var changes []Change

	for _, svc := range services {
		rendered, err := renderer.RenderEntry(svc)
		if err != nil {
			return nil, nil, fmt.Errorf("render %q: %w", svc.Name, err)
		}
		wantHash := contentHash(rendered)

		idx := findManagedIndex(entries, svc.ID)
		if idx < 0 {
			node, err := parseEntryNode(rendered)
			if err != nil {
				return nil, nil, fmt.Errorf("parse rendered entry for %q: %w", svc.Name, err)
			}
			entries = append(entries, seqEntry{comment: buildMarker(svc.ID, wantHash), value: node})
			changes = append(changes, Change{Kind: Added, ServiceID: svc.ID, ServiceName: svc.Name, Group: groupName})
			continue
		}

		m, _ := parseMarker(entries[idx].comment)
		currentBytes, err := renderer.NormalizeEntry(entries[idx].value)
		if err != nil {
			return nil, nil, fmt.Errorf("normalize existing entry for %q: %w", svc.Name, err)
		}
		handEdited := contentHash(currentBytes) != m.content

		if handEdited {
			switch opts.Conflict {
			case Fail:
				return nil, nil, &ConflictError{ServiceID: svc.ID, ServiceName: svc.Name}
			case Overwrite:
				// Proceed to overwrite below, same as an ordinary update.
			case Preserve:
				fallthrough
			default:
				changes = append(changes, Change{Kind: Conflict, ServiceID: svc.ID, ServiceName: svc.Name, Group: groupName})
				continue
			}
		}

		if bytes.Equal(currentBytes, rendered) {
			continue // already exactly what dashsync would write — true no-op
		}

		node, err := parseEntryNode(rendered)
		if err != nil {
			return nil, nil, fmt.Errorf("parse rendered entry for %q: %w", svc.Name, err)
		}
		entries[idx] = seqEntry{comment: buildMarker(svc.ID, wantHash), value: node}
		changes = append(changes, Change{Kind: Updated, ServiceID: svc.ID, ServiceName: svc.Name, Group: groupName})
	}

	return entries, changes, nil
}

// removeOrphaned drops every managed entry in entries whose service ID
// isn't mapped to groupName in desiredGroupOf — covering both "the
// service disappeared from Docker entirely" and "it moved to a different
// group." Unmanaged (unmarked) entries are never touched, regardless of
// their content.
//
// Removal is unconditional even for a hand-edited managed entry: once its
// service is gone (or has moved elsewhere), preserving the stale entry in
// its old spot doesn't serve any of ConflictPolicy's purposes — a human
// who wants to keep it can delete dashsync's marker comment, turning it
// into an entry dashsync no longer considers its own.
func removeOrphaned(entries []seqEntry, groupName string, desiredGroupOf map[string]string) ([]seqEntry, []Change) {
	var changes []Change

	kept := entries[:0]
	for _, e := range entries {
		m, ok := parseMarker(e.comment)
		if ok && desiredGroupOf[m.id] != groupName {
			changes = append(changes, Change{Kind: Removed, ServiceID: m.id, Group: groupName})
			continue
		}
		kept = append(kept, e)
	}
	return kept, changes
}

// findManagedIndex returns the index of the entry marked with id, or -1.
func findManagedIndex(entries []seqEntry, id string) int {
	for i, e := range entries {
		if m, ok := parseMarker(e.comment); ok && m.id == id {
			return i
		}
	}
	return -1
}
