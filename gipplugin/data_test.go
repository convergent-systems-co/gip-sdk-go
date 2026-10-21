package gipplugin

import (
	"context"
	"encoding/json"
	"io"
	"sort"
	"strings"
	"testing"

	"github.com/convergent-systems-co/gip-sdk-go/protocol"
)

const dataManifest = `{"schema":"gip.plugin-manifest/v1","name":"gip-plugin-t","version":"0.1.0","protocol_versions":[1],"executable":"t",
"permissions":["plugin-data"],
"commands":[{"name":"t","summary":"s","subcommands":[{"name":"run","summary":"s","permissions":["plugin-data"]}]}]}`

// memHost is a scripted host keeping plugin data in memory with the
// compare-and-swap semantics of PLG-028.
type memHost struct {
	vals      map[string][]byte
	vers      map[string]int64
	noProject bool
	calls     []string
}

func (h *memHost) answer(m *protocol.Message) *protocol.Message {
	h.calls = append(h.calls, m.Method)
	if h.noProject {
		return protocol.ErrorResponse(m.ID, protocol.NewError(protocol.CodeDataRefused, protocol.CodeNoProject, "no project is open"))
	}
	mismatch := func() *protocol.Message {
		return protocol.ErrorResponse(m.ID, protocol.NewError(protocol.CodeDataRefused, protocol.CodeDataVersionMismatch, "version mismatch"))
	}
	cas := func(key string, want *int64) bool { return want == nil || h.vers[key] == *want }
	var res any
	switch m.Method {
	case protocol.MethodHostPluginDataGet:
		var p protocol.PluginDataGetParams
		_ = json.Unmarshal(m.Params, &p)
		if v, ok := h.vals[p.Key]; ok {
			res = protocol.PluginDataGetResult{Found: true, Key: p.Key, Value: v, Version: h.vers[p.Key]}
		} else {
			res = protocol.PluginDataGetResult{}
		}
	case protocol.MethodHostPluginDataPut:
		var p protocol.PluginDataPutParams
		_ = json.Unmarshal(m.Params, &p)
		if !cas(p.Key, p.IfVersion) {
			return mismatch()
		}
		h.vals[p.Key] = p.Value
		h.vers[p.Key]++
		res = protocol.PluginDataPutResult{Version: h.vers[p.Key]}
	case protocol.MethodHostPluginDataDelete:
		var p protocol.PluginDataDeleteParams
		_ = json.Unmarshal(m.Params, &p)
		if !cas(p.Key, p.IfVersion) {
			return mismatch()
		}
		_, ok := h.vals[p.Key]
		delete(h.vals, p.Key)
		res = protocol.PluginDataDeleteResult{Deleted: ok}
	case protocol.MethodHostPluginDataList:
		var p protocol.PluginDataListParams
		_ = json.Unmarshal(m.Params, &p)
		var keys []string
		for k := range h.vals {
			if strings.HasPrefix(k, p.Prefix) && k > p.After {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		out := protocol.PluginDataListResult{Entries: []protocol.PluginDataEntry{}}
		limit := p.Limit
		if limit == 0 {
			limit = protocol.DefaultPluginDataListLimit
		}
		for i, k := range keys {
			if i == limit {
				out.Next = keys[i-1]
				break
			}
			out.Entries = append(out.Entries, protocol.PluginDataEntry{Key: k, Version: h.vers[k], Size: len(h.vals[k])})
		}
		res = out
	default:
		return protocol.ErrorResponse(m.ID, protocol.NewError(protocol.CodeMethodNotFound, "host.method_not_found", m.Method))
	}
	resp, _ := protocol.Response(m.ID, res)
	return resp
}

// runData serves one invocation of handler with the given grants against
// host and returns the invoke response.
func runData(t *testing.T, granted []string, host *memHost, handler Handler) *protocol.Message {
	t.Helper()
	hostIn, pluginOut := io.Pipe()
	pluginIn, hostOut := io.Pipe()
	done := make(chan int, 1)
	go func() {
		done <- ServeIO(context.Background(), []byte(dataManifest), Handlers{"t run": handler}, pluginIn, pluginOut)
	}()
	conn := protocol.NewConn(hostIn, hostOut)
	send := func(m *protocol.Message) {
		if err := conn.Write(m); err != nil {
			t.Fatal(err)
		}
	}
	init, _ := protocol.Request(1, protocol.MethodInitialize, protocol.InitializeParams{ProtocolVersions: []int{1}, GrantedPermissions: granted})
	send(init)
	if m, err := conn.Read(); err != nil || m.Error != nil {
		t.Fatalf("initialize: %+v %v", m, err)
	}
	inv, _ := protocol.Request(2, protocol.MethodInvoke, protocol.InvokeParams{Command: []string{"t", "run"}, Arguments: map[string]any{}})
	send(inv)
	for {
		m, err := conn.Read()
		if err != nil {
			t.Fatal(err)
		}
		if m.Method == "" {
			sd, _ := protocol.Notification(protocol.MethodShutdown, struct{}{})
			send(sd)
			if code := <-done; code != 0 {
				t.Fatalf("exit %d", code)
			}
			return m
		}
		if m.IsRequest() {
			send(host.answer(m))
		}
	}
}

// TestPluginData drives get, put, delete and list through ServeIO with a
// scripted host, including compare-and-swap (PLG-028, ADR-0025 C16).
func TestPluginData(t *testing.T) {
	host := &memHost{vals: map[string][]byte{}, vers: map[string]int64{}}
	var log []string
	note := func(s string) { log = append(log, s) }
	handler := func(ctx context.Context, r *Request) (*protocol.InvokeResult, error) {
		g, err := r.DataGet(ctx, "lease/w-1")
		if err != nil || g.Found {
			t.Errorf("get missing: %+v %v", g, err)
		}
		v1, err := r.DataPut(ctx, "lease/w-1", []byte("held"), protocol.IfVersion(0))
		if err != nil || v1 != 1 {
			t.Errorf("create: %d %v", v1, err)
		}
		if _, err := r.DataPut(ctx, "lease/w-1", []byte("again"), protocol.IfVersion(0)); !protocol.IsVersionMismatch(err) {
			t.Errorf("a second create must be DATA_VERSION_MISMATCH: %v", err)
		}
		v2, err := r.DataPut(ctx, "lease/w-1", []byte("renewed"), protocol.IfVersion(v1))
		if err != nil || v2 != 2 {
			t.Errorf("cas put: %d %v", v2, err)
		}
		if _, err := r.DataPut(ctx, "lease/w-1", []byte("stale"), protocol.IfVersion(v1)); !protocol.IsVersionMismatch(err) {
			t.Errorf("a stale cas must be DATA_VERSION_MISMATCH: %v", err)
		}
		g, err = r.DataGet(ctx, "lease/w-1")
		if err != nil || !g.Found || string(g.Value) != "renewed" || g.Version != 2 {
			t.Errorf("get: %+v %v", g, err)
		}
		type memo struct{ Notes []string }
		if _, err := r.DataPutJSON(ctx, "memo/w-1", memo{Notes: []string{"a"}}, nil); err != nil {
			t.Error(err)
		}
		var mm memo
		if v, err := r.DataGetJSON(ctx, "memo/w-1", &mm); err != nil || v != 1 || mm.Notes[0] != "a" {
			t.Errorf("json: %d %+v %v", v, mm, err)
		}
		for _, k := range []string{"lease/w-2", "lease/w-3"} {
			if _, err := r.DataPut(ctx, k, nil, nil); err != nil {
				t.Error(err)
			}
		}
		var keys []string
		page := protocol.PluginDataListParams{Prefix: "lease/", Limit: 2}
		for {
			l, err := r.DataList(ctx, page)
			if err != nil {
				return nil, err
			}
			for _, e := range l.Entries {
				keys = append(keys, e.Key)
			}
			if l.Next == "" {
				break
			}
			page.After = l.Next
		}
		note("list=" + strings.Join(keys, ","))
		if _, err := r.DataDelete(ctx, "lease/w-1", protocol.IfVersion(1)); !protocol.IsVersionMismatch(err) {
			t.Errorf("stale delete: %v", err)
		}
		if ok, err := r.DataDelete(ctx, "lease/w-1", protocol.IfVersion(2)); err != nil || !ok {
			t.Errorf("delete: %v %v", ok, err)
		}
		if ok, err := r.DataDelete(ctx, "lease/w-1", nil); err != nil || ok {
			t.Errorf("delete absent: %v %v", ok, err)
		}
		return &protocol.InvokeResult{Text: "ok"}, nil
	}
	if m := runData(t, []string{protocol.PermPluginData}, host, handler); m.Error != nil {
		t.Fatal(m.Error)
	}
	if len(log) != 1 || log[0] != "list=lease/w-1,lease/w-2,lease/w-3" {
		t.Fatalf("log %q", log)
	}
}

// TestPluginDataIsGatedAndValidated: without plugin-data, or with a
// request breaking the PLG-028 limits, nothing is sent to the host.
func TestPluginDataIsGatedAndValidated(t *testing.T) {
	host := &memHost{vals: map[string][]byte{}, vers: map[string]int64{}}
	handler := func(ctx context.Context, r *Request) (*protocol.InvokeResult, error) {
		_, err := r.DataGet(ctx, "k")
		if protocol.ErrorID(err) != protocol.ErrorIDPermissionDenied {
			t.Errorf("without plugin-data: %v", err)
		}
		if _, err := r.DataPut(ctx, "k", nil, nil); protocol.ErrorID(err) != protocol.ErrorIDPermissionDenied {
			t.Errorf("put without plugin-data: %v", err)
		}
		return nil, nil
	}
	runData(t, []string{"work.read"}, host, handler)
	handler = func(ctx context.Context, r *Request) (*protocol.InvokeResult, error) {
		if _, err := r.DataPut(ctx, strings.Repeat("k", 201), nil, nil); protocol.ErrorID(err) != protocol.CodeDataKeyInvalid {
			t.Errorf("long key: %v", err)
		}
		if _, err := r.DataPut(ctx, "k", make([]byte, protocol.MaxPluginDataValueBytes+1), nil); protocol.ErrorID(err) != protocol.CodeDataValueTooLarge {
			t.Errorf("large value: %v", err)
		}
		if _, err := r.DataList(ctx, protocol.PluginDataListParams{Limit: protocol.MaxPluginDataListLimit + 1}); protocol.ErrorID(err) != protocol.CodeDataLimitInvalid {
			t.Errorf("list limit: %v", err)
		}
		if _, err := r.DataDelete(ctx, "\x00", nil); protocol.ErrorID(err) != protocol.CodeDataKeyInvalid {
			t.Errorf("bad key: %v", err)
		}
		return nil, nil
	}
	runData(t, []string{protocol.PermPluginData}, host, handler)
	if len(host.calls) != 0 {
		t.Fatalf("refused requests reached the host: %q", host.calls)
	}
}

func TestPluginDataWithoutAProject(t *testing.T) {
	host := &memHost{noProject: true}
	handler := func(ctx context.Context, r *Request) (*protocol.InvokeResult, error) {
		if _, err := r.DataGet(ctx, "k"); !protocol.IsNoProject(err) {
			t.Errorf("no project: %v", err)
		}
		return nil, nil
	}
	runData(t, []string{protocol.PermPluginData}, host, handler)
}
