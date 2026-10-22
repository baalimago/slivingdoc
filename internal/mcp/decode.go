package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/strictjson"
)

// maxMessageBytes is the documented message bound
// (architecture/product-contract.md).
const maxMessageBytes = notebook.MaxMessageBytes

// decodePull strictly decodes the raw notes_pull arguments: a JSON object
// with no fields. The server serves one notebook directory, so a present
// path member is refused before any other member is considered.
func decodePull(raw json.RawMessage) error {
	if err := rejectPath(raw); err != nil {
		return err
	}
	v, err := parseArguments(raw)
	if err != nil {
		return err
	}
	return v.RejectUnknown()
}

// emptyArguments is the argument object an absent or null arguments member
// stands for.
var emptyArguments = json.RawMessage("{}")

// parseArguments strictly parses the raw arguments member into an object.
// An absent or null member is the empty object (architecture/mcp-server.md):
// notes_pull then pulls the server's notebook directory, and notes_commit
// reports the missing message.
func parseArguments(raw json.RawMessage) (strictjson.Value, error) {
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		raw = emptyArguments
	}
	v, err := strictjson.Parse(raw)
	if err != nil {
		return strictjson.Value{}, fmt.Errorf("arguments are not a strict JSON object: %w", err)
	}
	if v.Kind != strictjson.Object {
		return strictjson.Value{}, errors.New("arguments must be a JSON object")
	}
	return v, nil
}

// rejectPath refuses a request that names the removed per-call path. The
// check reads the raw document before the strict parser, because the
// strict parser rejects an explicit null before any field can be named;
// the refusal must also precede the unknown-member one. A document this
// check cannot decode is left to parseArguments.
func rejectPath(raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return nil
	}
	if _, ok := fields["path"]; !ok {
		return nil
	}
	return errors.New("the notes tools take no path; this server serves the directory named by --workspace-root")
}

// decodeCommit strictly decodes the raw notes_commit arguments: a JSON
// object with the required "message" and no other field. The message is
// checked by notebook.ValidateMessage so it fails with the notebook's own
// reason.
func decodeCommit(raw json.RawMessage) (string, error) {
	if err := rejectPath(raw); err != nil {
		return "", err
	}
	v, err := parseArguments(raw)
	if err != nil {
		return "", err
	}
	if err := v.RejectUnknown("message"); err != nil {
		return "", err
	}
	messageField, ok := v.Field("message")
	if !ok {
		return "", errors.New("message is required")
	}
	if messageField.Kind != strictjson.String {
		return "", errors.New("message must be a string")
	}
	if err := notebook.ValidateMessage(messageField.Str); err != nil {
		return "", err
	}
	return messageField.Str, nil
}
