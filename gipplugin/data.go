package gipplugin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/convergent-systems-co/gip-sdk-go/protocol"
)

// Plugin data (SDK v0.10.0, gip ADR-0025 D12, PLG-028): a small key-value
// store per plugin and project, kept by the host where neither the
// plugin's sandbox nor its child processes can reach it. It is not
// governed; losing it means starting over. Every helper requires the
// plugin-data permission: without it the helper returns a
// host.permission_denied error and sends nothing. A request whose shape
// breaks the PLG-028 limits (protocol.MaxPluginDataKeyBytes and friends)
// is an error and is not sent. Match host refusals with
// protocol.IsVersionMismatch, protocol.IsNoProject and protocol.ErrorID.

func (r *Request) needData(method string) error {
	if !r.Has(protocol.PermPluginData) {
		return protocol.NewError(protocol.CodePermissionDenied, protocol.ErrorIDPermissionDenied,
			fmt.Sprintf("%s requires permission %s", method, protocol.PermPluginData))
	}
	return nil
}

// DataGet reads key. A missing key is a result with Found false, not an
// error.
func (r *Request) DataGet(ctx context.Context, key string) (*protocol.PluginDataGetResult, error) {
	p := protocol.PluginDataGetParams{Key: key}
	if err := r.needData(protocol.MethodHostPluginDataGet); err != nil {
		return nil, err
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	var out protocol.PluginDataGetResult
	if err := r.session.call(ctx, protocol.MethodHostPluginDataGet, p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DataPut writes value under key and returns the new version. ifVersion
// makes it a compare-and-swap: nil writes unconditionally,
// protocol.IfVersion(0) only creates, protocol.IfVersion(n) replaces only
// version n. A mismatch is a DATA_VERSION_MISMATCH error
// (protocol.IsVersionMismatch) and writes nothing.
func (r *Request) DataPut(ctx context.Context, key string, value []byte, ifVersion *int64) (int64, error) {
	if value == nil {
		value = []byte{}
	}
	p := protocol.PluginDataPutParams{Key: key, Value: value, IfVersion: ifVersion}
	if err := r.needData(protocol.MethodHostPluginDataPut); err != nil {
		return 0, err
	}
	if err := p.Validate(); err != nil {
		return 0, err
	}
	var out protocol.PluginDataPutResult
	if err := r.session.call(ctx, protocol.MethodHostPluginDataPut, p, &out); err != nil {
		return 0, err
	}
	return out.Version, nil
}

// DataDelete deletes key and reports whether it existed. ifVersion as for
// DataPut.
func (r *Request) DataDelete(ctx context.Context, key string, ifVersion *int64) (bool, error) {
	p := protocol.PluginDataDeleteParams{Key: key, IfVersion: ifVersion}
	if err := r.needData(protocol.MethodHostPluginDataDelete); err != nil {
		return false, err
	}
	if err := p.Validate(); err != nil {
		return false, err
	}
	var out protocol.PluginDataDeleteResult
	if err := r.session.call(ctx, protocol.MethodHostPluginDataDelete, p, &out); err != nil {
		return false, err
	}
	return out.Deleted, nil
}

// DataList lists one page of keys (without values) in byte order; pass
// the result's Next as the next page's After.
func (r *Request) DataList(ctx context.Context, p protocol.PluginDataListParams) (*protocol.PluginDataListResult, error) {
	if err := r.needData(protocol.MethodHostPluginDataList); err != nil {
		return nil, err
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	var out protocol.PluginDataListResult
	if err := r.session.call(ctx, protocol.MethodHostPluginDataList, p, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DataGetJSON reads key and decodes its value as JSON into out, returning
// the version, or 0 when the key does not exist (out untouched).
func (r *Request) DataGetJSON(ctx context.Context, key string, out any) (int64, error) {
	res, err := r.DataGet(ctx, key)
	if err != nil || !res.Found {
		return 0, err
	}
	if err := json.Unmarshal(res.Value, out); err != nil {
		return 0, fmt.Errorf("plugin data %q: %w", key, err)
	}
	return res.Version, nil
}

// DataPutJSON encodes v as JSON and writes it under key, as DataPut.
func (r *Request) DataPutJSON(ctx context.Context, key string, v any, ifVersion *int64) (int64, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return 0, err
	}
	return r.DataPut(ctx, key, b, ifVersion)
}
