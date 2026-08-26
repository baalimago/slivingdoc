package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/pathutil"
	"github.com/baalimago/slivingdoc/internal/strictjson"
)

// The documented byte bounds of the tool inputs (architecture section 2):
// path holds 1 through 4,096 bytes after a leading home abbreviation is
// expanded; message holds at most 16,384 bytes.
const (
	maxPathBytes    = 4096
	maxMessageBytes = notebook.MaxMessageBytes
)

// decodePull strictly decodes the raw notes_pull arguments: a JSON object
// with exactly the "path" field, a non-null string, an absolute UTF-8 host
// path or a leading ~/ abbreviation, of at most 4,096 bytes without U+0000.
// Unknown fields, duplicate fields, explicit null, non-object arguments, and
// malformed JSON are
// rejected by the strict parser before any semantic check.
func decodePull(raw json.RawMessage) (string, error) {
	v, err := strictjson.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("arguments are not a strict JSON object: %w", err)
	}
	if v.Kind != strictjson.Object {
		return "", errors.New("arguments must be a JSON object")
	}
	if err := v.RejectUnknown("path"); err != nil {
		return "", err
	}
	field, ok := v.Field("path")
	if !ok {
		return "", errors.New("path is required")
	}
	if field.Kind != strictjson.String {
		return "", errors.New("path must be a string")
	}
	return validatePath(field.Str)
}

// decodeCommit strictly decodes the raw notes_commit arguments: a JSON
// object with exactly "path" and "message", both non-null strings. The
// message is non-blank UTF-8 of at most 16,384 bytes without U+0000.
func decodeCommit(raw json.RawMessage) (path, message string, err error) {
	v, err := strictjson.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("arguments are not a strict JSON object: %w", err)
	}
	if v.Kind != strictjson.Object {
		return "", "", errors.New("arguments must be a JSON object")
	}
	if err := v.RejectUnknown("path", "message"); err != nil {
		return "", "", err
	}
	pathField, ok := v.Field("path")
	if !ok {
		return "", "", errors.New("path is required")
	}
	if pathField.Kind != strictjson.String {
		return "", "", errors.New("path must be a string")
	}
	messageField, ok := v.Field("message")
	if !ok {
		return "", "", errors.New("message is required")
	}
	if messageField.Kind != strictjson.String {
		return "", "", errors.New("message must be a string")
	}
	path, err = validatePath(pathField.Str)
	if err != nil {
		return "", "", err
	}
	if err := validateMessage(messageField.Str); err != nil {
		return "", "", err
	}
	return path, messageField.Str, nil
}

// validatePath applies the path contract: a leading home abbreviation is
// expanded, then the result must be an absolute UTF-8 host path with 1 through
// 4,096 bytes, without U+0000. The path must stay below the configured
// workspace root; the service enforces that rule when it resolves the request.
func validatePath(s string) (string, error) {
	if s == "" {
		return "", errors.New("path must not be empty")
	}
	var err error
	if s, err = pathutil.ExpandHome(s); err != nil {
		return "", err
	}
	if len(s) > maxPathBytes {
		return "", fmt.Errorf("path exceeds %d bytes", maxPathBytes)
	}
	if !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
		return "", errors.New("path must be valid UTF-8 without U+0000")
	}
	if !filepath.IsAbs(s) {
		return "", errors.New("path must be absolute")
	}
	return s, nil
}

// validateMessage applies the message contract: non-blank (not only
// Unicode white space), valid UTF-8, at most 16,384 bytes, without U+0000.
func validateMessage(s string) error {
	if len(s) > maxMessageBytes {
		return fmt.Errorf("message exceeds %d bytes", maxMessageBytes)
	}
	if !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
		return errors.New("message must be valid UTF-8 without U+0000")
	}
	if strings.TrimSpace(s) == "" {
		return errors.New("message must not be blank")
	}
	return nil
}
