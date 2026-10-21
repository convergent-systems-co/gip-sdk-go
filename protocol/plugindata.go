package protocol

import (
	"errors"
	"fmt"
)

// Plugin data (additive in SDK v0.10.0; gip ADR-0025 D12 and §8, spec
// PLG-028, #61).
//
// With the plugin-data permission (PermPluginData) granted, a plugin keeps
// a small key-value store through the host: get, put, delete and list,
// with compare-and-swap on a per-key version. The namespace is the
// plugin's short name and the project, so no plugin sees another's data,
// and the data is held in the project's .gip/state.db, which no sandbox
// declaration makes readable or writable: the JSON-RPC channel is the only
// way in. A plugin must therefore never pass the channel (its stdin and
// stdout) to processes it starts (gipplugin.Command).
//
// The data is not governed: it is outside the ledger, not committed, and
// losing it means starting over. A projection rebuild keeps it; deleting
// state.db loses it.
//
// A gip that predates it answers the methods with method-not-found
// (CodeMethodNotFound). Request.DataGet and friends validate keys, values
// and list bounds against the limits below before sending, so a request
// the host would refuse for its shape is an error and is not sent; the
// host's own check is authoritative, and only the host enforces the
// per-project quotas.
//
// Versions come from one monotonic counter per plugin and project: every
// write in the namespace takes the next value, and a key's version is the
// value of its last write. No version is ever issued twice in a
// namespace, so a version is never reused across delete and re-create (no
// per-key tombstones are needed), and a stale IfVersion never matches a
// key that was deleted and written again (no ABA). Versions of different
// keys are comparable only as "written before or after"; they are not
// consecutive per key.
//
// The contract core H must implement (SDK v0.10.0; beyond the PLG-028
// text, decided for gip's host side): the refusal ids NO_PROJECT and DATA_QUOTA_EXCEEDED
// with error code CodeDataRefused (-31004), the invalid-request ids
// DATA_KEY_INVALID, DATA_VALUE_TOO_LARGE and DATA_LIMIT_INVALID, the list
// page sizes DefaultPluginDataListLimit and MaxPluginDataListLimit,
// IfVersion 0 meaning create-only (and DATA_VERSION_MISMATCH on a
// delete of an existing key), and the per-namespace version counter.
const (
	MethodHostPluginDataGet    = "host/plugin/data/get"    // plugin -> host, request (plugin-data)
	MethodHostPluginDataPut    = "host/plugin/data/put"    // plugin -> host, request (plugin-data)
	MethodHostPluginDataDelete = "host/plugin/data/delete" // plugin -> host, request (plugin-data)
	MethodHostPluginDataList   = "host/plugin/data/list"   // plugin -> host, request (plugin-data)
)

// PluginDataMethods returns the plugin data methods, every one gated by
// PermPluginData.
func PluginDataMethods() []string {
	return []string{MethodHostPluginDataGet, MethodHostPluginDataPut, MethodHostPluginDataDelete, MethodHostPluginDataList}
}

// Plugin data limits (PLG-028). Keys and list prefixes are printable ASCII
// (0x20 to 0x7E); a key is 1 to MaxPluginDataKeyBytes bytes; a value is at
// most MaxPluginDataValueBytes bytes (before the base64 the JSON encoding
// applies); one plugin holds at most MaxPluginDataKeys keys and
// MaxPluginDataBytes bytes per project (DATA_QUOTA_EXCEEDED, enforced by
// the host only).
const (
	MaxPluginDataKeyBytes   = 200
	MaxPluginDataValueBytes = 1 << 20  // 1 MiB
	MaxPluginDataKeys       = 4096     // per plugin per project
	MaxPluginDataBytes      = 64 << 20 // 64 MiB per plugin per project
	// DefaultPluginDataListLimit is the page size of a list without a
	// limit, and MaxPluginDataListLimit the largest page a list may ask
	// for (the contract core H must implement; a larger limit is
	// DATA_LIMIT_INVALID).
	DefaultPluginDataListLimit = 256
	MaxPluginDataListLimit     = 1024
)

// Stable error ids (ErrorData.ID) of the plugin data methods. Use ErrorID
// or IsErrorID to match them.
const (
	// CodeDataVersionMismatch: the compare-and-swap of a put or delete
	// failed (IfVersion did not match the key's current version, or the
	// key exists when IfVersion is 0). Nothing was written; read the key
	// again and retry. Error code CodeDataRefused.
	CodeDataVersionMismatch = "DATA_VERSION_MISMATCH"
	// CodeDataQuotaExceeded (the contract core H must implement): the put would take the plugin past
	// MaxPluginDataKeys keys or MaxPluginDataBytes bytes in the project.
	// Nothing was written. Error code CodeDataRefused.
	CodeDataQuotaExceeded = "DATA_QUOTA_EXCEEDED"
	// CodeDataKeyInvalid: a key, prefix or list cursor is empty where a
	// key is required, longer than MaxPluginDataKeyBytes or not printable
	// ASCII. Error code CodeInvalidParams.
	CodeDataKeyInvalid = "DATA_KEY_INVALID"
	// CodeDataValueTooLarge: a value exceeds MaxPluginDataValueBytes.
	// Error code CodeInvalidParams.
	CodeDataValueTooLarge = "DATA_VALUE_TOO_LARGE"
	// CodeDataLimitInvalid: a list limit is negative or above
	// MaxPluginDataListLimit, or IfVersion is negative. Error code
	// CodeInvalidParams.
	CodeDataLimitInvalid = "DATA_LIMIT_INVALID"
	// CodeNoProject (the contract core H must implement): no project is open, so there is no namespace to read
	// or write. Error code CodeDataRefused. A gip that predates plugin data
	// reports this condition for other methods as host.unavailable
	// (IsNoProject accepts both).
	CodeNoProject = "NO_PROJECT"
	// ErrorIDHostUnavailable is the id gip uses for "no project is open"
	// on the methods that predate plugin data.
	ErrorIDHostUnavailable = "host.unavailable"
	// ErrorIDPermissionDenied is the id of a host method called without
	// its permission (code CodePermissionDenied).
	ErrorIDPermissionDenied = "host.permission_denied"
)

// CodeDataRefused (-31004; the contract core H must implement) is the JSON-RPC error code of a plugin data
// request the host understood and refused: DATA_VERSION_MISMATCH,
// DATA_QUOTA_EXCEEDED and NO_PROJECT.
const CodeDataRefused = -31004

// PluginDataGetParams reads one key.
type PluginDataGetParams struct {
	Key string `json:"key"`
}

// PluginDataGetResult is the key's value and version; Found is false (and
// the rest empty) when the key does not exist.
type PluginDataGetResult struct {
	Found   bool   `json:"found"`
	Key     string `json:"key,omitempty"`
	Value   []byte `json:"value,omitempty"`
	Version int64  `json:"version,omitempty"`
}

// PluginDataPutParams writes one key. IfVersion makes it a
// compare-and-swap: nil writes unconditionally, 0 writes only when the key
// does not exist, never written or deleted (create-only; the contract
// core H must implement), and n > 0 writes only when the key's current
// version is n.
// A mismatch is DATA_VERSION_MISMATCH and writes nothing. An empty Value is
// a valid value.
//
// Versions come from one monotonic counter per plugin and project, so a
// version is never reused: a version read before a delete never matches
// the re-created key.
type PluginDataPutParams struct {
	Key       string `json:"key"`
	Value     []byte `json:"value"`
	IfVersion *int64 `json:"if_version,omitempty"`
}

// PluginDataPutResult is the key's version after the write: the next value
// of the plugin's per-project counter, always positive and above every
// version issued before in the namespace, deletions included.
type PluginDataPutResult struct {
	Version int64 `json:"version"`
}

// PluginDataDeleteParams deletes one key. IfVersion as for put: nil
// deletes unconditionally, n > 0 only when the current version is n. 0
// matches only an absent key: on an existing key it is
// DATA_VERSION_MISMATCH and deletes nothing; on an absent key it deletes
// nothing and reports Deleted false.
type PluginDataDeleteParams struct {
	Key       string `json:"key"`
	IfVersion *int64 `json:"if_version,omitempty"`
}

// PluginDataDeleteResult says whether a key was deleted; deleting an
// absent key without IfVersion is not an error (Deleted false).
type PluginDataDeleteResult struct {
	Deleted bool `json:"deleted"`
}

// PluginDataListParams lists keys in byte order: those starting with
// Prefix (all when empty), strictly after After (a cursor: the Next of
// the previous page), at most Limit of them (0 means
// DefaultPluginDataListLimit).
type PluginDataListParams struct {
	Prefix string `json:"prefix,omitempty"`
	After  string `json:"after,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// PluginDataEntry is one listed key: its version and value size in bytes,
// without the value.
type PluginDataEntry struct {
	Key     string `json:"key"`
	Version int64  `json:"version"`
	Size    int    `json:"size"`
}

// PluginDataListResult is one page of keys. Next, when not empty, is the
// After of the next page; it is empty on the last page.
type PluginDataListResult struct {
	Entries []PluginDataEntry `json:"entries"`
	Next    string            `json:"next,omitempty"`
}

// IfVersion returns a pointer to v, for the IfVersion of a put or delete:
// IfVersion(0) is create-only, IfVersion(n) matches only current version
// n. Versions come from one counter per plugin and project, so none is
// ever reused, even after a delete.
func IfVersion(v int64) *int64 { return &v }

// ValidPluginDataKey reports why key is not a valid plugin data key: it
// must be 1 to MaxPluginDataKeyBytes bytes of printable ASCII.
func ValidPluginDataKey(key string) error {
	if key == "" {
		return dataError(CodeInvalidParams, CodeDataKeyInvalid, "a plugin data key must not be empty")
	}
	return validDataText("key", key)
}

func validDataText(what, s string) error {
	if len(s) > MaxPluginDataKeyBytes {
		return dataError(CodeInvalidParams, CodeDataKeyInvalid, fmt.Sprintf("a plugin data %s is at most %d bytes (got %d)", what, MaxPluginDataKeyBytes, len(s)))
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 || c > 0x7e {
			return dataError(CodeInvalidParams, CodeDataKeyInvalid, fmt.Sprintf("a plugin data %s is printable ASCII (byte %d is 0x%02x)", what, i, c))
		}
	}
	return nil
}

func validIfVersion(v *int64) error {
	if v != nil && *v < 0 {
		return dataError(CodeInvalidParams, CodeDataLimitInvalid, fmt.Sprintf("if_version must not be negative (got %d)", *v))
	}
	return nil
}

// Validate checks the request's shape against PLG-028.
func (p *PluginDataGetParams) Validate() error { return ValidPluginDataKey(p.Key) }

// Validate checks the request's shape against PLG-028.
func (p *PluginDataPutParams) Validate() error {
	if err := ValidPluginDataKey(p.Key); err != nil {
		return err
	}
	if len(p.Value) > MaxPluginDataValueBytes {
		return dataError(CodeInvalidParams, CodeDataValueTooLarge, fmt.Sprintf("a plugin data value is at most %d bytes (got %d)", MaxPluginDataValueBytes, len(p.Value)))
	}
	return validIfVersion(p.IfVersion)
}

// Validate checks the request's shape against PLG-028.
func (p *PluginDataDeleteParams) Validate() error {
	if err := ValidPluginDataKey(p.Key); err != nil {
		return err
	}
	return validIfVersion(p.IfVersion)
}

// Validate checks the request's shape against PLG-028.
func (p *PluginDataListParams) Validate() error {
	if err := validDataText("prefix", p.Prefix); err != nil {
		return err
	}
	if err := validDataText("cursor", p.After); err != nil {
		return err
	}
	if p.Limit < 0 || p.Limit > MaxPluginDataListLimit {
		return dataError(CodeInvalidParams, CodeDataLimitInvalid, fmt.Sprintf("a list limit is 0 (default %d) to %d (got %d)", DefaultPluginDataListLimit, MaxPluginDataListLimit, p.Limit))
	}
	return nil
}

func dataError(code int, id, msg string) *Error { return NewError(code, id, msg) }

// ErrorID returns the stable id (ErrorData.ID) of a protocol error
// anywhere in err's chain, or "".
func ErrorID(err error) string {
	var pe *Error
	if errors.As(err, &pe) && pe.Data != nil {
		return pe.Data.ID
	}
	return ""
}

// IsErrorID reports whether err is a protocol error with stable id id.
func IsErrorID(err error, id string) bool { return id != "" && ErrorID(err) == id }

// IsVersionMismatch reports whether err is a failed compare-and-swap
// (DATA_VERSION_MISMATCH).
func IsVersionMismatch(err error) bool { return IsErrorID(err, CodeDataVersionMismatch) }

// IsNoProject reports whether err says no project is open: NO_PROJECT, or
// host.unavailable from a gip that predates it.
func IsNoProject(err error) bool {
	return IsErrorID(err, CodeNoProject) || IsErrorID(err, ErrorIDHostUnavailable)
}
