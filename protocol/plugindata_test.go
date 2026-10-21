package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestPluginDataLimitsMatchTheSpec(t *testing.T) {
	// PLG-028: keys at most 200 bytes, values at most 1 MiB, 4,096 keys and
	// 64 MiB per plugin per project.
	if MaxPluginDataKeyBytes != 200 || MaxPluginDataValueBytes != 1048576 || MaxPluginDataKeys != 4096 || MaxPluginDataBytes != 67108864 {
		t.Fatal("plugin data limits drifted from PLG-028")
	}
	if !slices.Contains(KnownPermissions, PermPluginData) || PermPluginData != "plugin-data" {
		t.Fatal("plugin-data must be a known permission")
	}
	want := []string{"host/plugin/data/get", "host/plugin/data/put", "host/plugin/data/delete", "host/plugin/data/list"}
	if !slices.Equal(PluginDataMethods(), want) {
		t.Fatalf("methods %q", PluginDataMethods())
	}
	if CodeDataVersionMismatch != "DATA_VERSION_MISMATCH" {
		t.Fatal("the compare-and-swap code is DATA_VERSION_MISMATCH (ADR-0025 §8)")
	}
}

func TestPluginDataKeyValidation(t *testing.T) {
	for _, tc := range []struct {
		key string
		ok  bool
	}{
		{"a", true},
		{"lease/w-1 notes", true},
		{"~!@#$%^&*()_+{}|:\"<>?`-=[]\\;',./", true},
		{strings.Repeat("k", MaxPluginDataKeyBytes), true},
		{"", false},
		{strings.Repeat("k", MaxPluginDataKeyBytes+1), false},
		{"tab\there", false},
		{"nl\n", false},
		{"del\x7f", false},
		{"é", false},
		{"nul\x00", false},
	} {
		err := ValidPluginDataKey(tc.key)
		if (err == nil) != tc.ok {
			t.Errorf("key %q: err %v, want ok=%v", tc.key, err, tc.ok)
		}
		if err != nil && (ErrorID(err) != CodeDataKeyInvalid || !isCode(err, CodeInvalidParams)) {
			t.Errorf("key %q: %v is not %s", tc.key, err, CodeDataKeyInvalid)
		}
	}
}

func isCode(err error, code int) bool {
	var pe *Error
	return errors.As(err, &pe) && pe.Code == code
}

func TestPluginDataRequestValidation(t *testing.T) {
	big := make([]byte, MaxPluginDataValueBytes)
	for _, tc := range []struct {
		name string
		v    interface{ Validate() error }
		id   string
	}{
		{"put max value", &PluginDataPutParams{Key: "k", Value: big}, ""},
		{"put empty value", &PluginDataPutParams{Key: "k", Value: []byte{}}, ""},
		{"put create", &PluginDataPutParams{Key: "k", IfVersion: IfVersion(0)}, ""},
		{"put value too large", &PluginDataPutParams{Key: "k", Value: append(big, 0)}, CodeDataValueTooLarge},
		{"put negative version", &PluginDataPutParams{Key: "k", IfVersion: IfVersion(-1)}, CodeDataLimitInvalid},
		{"put bad key", &PluginDataPutParams{Key: ""}, CodeDataKeyInvalid},
		{"get bad key", &PluginDataGetParams{Key: "a\x01"}, CodeDataKeyInvalid},
		{"delete cas", &PluginDataDeleteParams{Key: "k", IfVersion: IfVersion(3)}, ""},
		{"delete negative version", &PluginDataDeleteParams{Key: "k", IfVersion: IfVersion(-2)}, CodeDataLimitInvalid},
		{"list all", &PluginDataListParams{}, ""},
		{"list max", &PluginDataListParams{Prefix: "lease/", After: "lease/a", Limit: MaxPluginDataListLimit}, ""},
		{"list limit too large", &PluginDataListParams{Limit: MaxPluginDataListLimit + 1}, CodeDataLimitInvalid},
		{"list negative limit", &PluginDataListParams{Limit: -1}, CodeDataLimitInvalid},
		{"list prefix too long", &PluginDataListParams{Prefix: strings.Repeat("p", MaxPluginDataKeyBytes+1)}, CodeDataKeyInvalid},
		{"list bad cursor", &PluginDataListParams{After: "\n"}, CodeDataKeyInvalid},
	} {
		err := tc.v.Validate()
		if got := ErrorID(err); got != tc.id {
			t.Errorf("%s: error id %q (%v), want %q", tc.name, got, err, tc.id)
		}
	}
}

func TestPluginDataWireShape(t *testing.T) {
	b, _ := json.Marshal(PluginDataPutParams{Key: "k", Value: []byte("hi")})
	if string(b) != `{"key":"k","value":"aGk="}` {
		t.Fatalf("unconditional put: %s", b)
	}
	b, _ = json.Marshal(PluginDataPutParams{Key: "k", Value: []byte{}, IfVersion: IfVersion(0)})
	if string(b) != `{"key":"k","value":"","if_version":0}` {
		t.Fatalf("create-only put must send if_version 0: %s", b)
	}
	b, _ = json.Marshal(PluginDataDeleteParams{Key: "k", IfVersion: IfVersion(7)})
	if string(b) != `{"key":"k","if_version":7}` {
		t.Fatalf("delete: %s", b)
	}
	b, _ = json.Marshal(PluginDataListParams{})
	if string(b) != `{}` {
		t.Fatalf("list: %s", b)
	}
	var g PluginDataGetResult
	if err := json.Unmarshal([]byte(`{"found":true,"key":"k","value":"aGk=","version":4}`), &g); err != nil || string(g.Value) != "hi" || g.Version != 4 {
		t.Fatalf("get result: %+v %v", g, err)
	}
}

func TestErrorIDMatching(t *testing.T) {
	cas := NewError(CodeDataRefused, CodeDataVersionMismatch, "version 3, have 4")
	wrapped := fmt.Errorf("saving: %w", cas)
	if !IsVersionMismatch(wrapped) || ErrorID(wrapped) != CodeDataVersionMismatch {
		t.Fatal("a wrapped DATA_VERSION_MISMATCH must match")
	}
	if IsVersionMismatch(errors.New("DATA_VERSION_MISMATCH")) || ErrorID(nil) != "" || IsErrorID(&Error{Code: 1}, "") {
		t.Fatal("only structured errors match")
	}
	if !IsNoProject(NewError(CodeDataRefused, CodeNoProject, "")) || !IsNoProject(NewError(CodeInternal, ErrorIDHostUnavailable, "")) || IsNoProject(cas) {
		t.Fatal("IsNoProject matches NO_PROJECT and the older host.unavailable only")
	}
}
