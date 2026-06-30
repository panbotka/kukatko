package savedsearch

import (
	"encoding/json"
	"testing"
)

// TestDefaultParams checks that nil/empty params collapse to the empty JSON
// object while non-empty params pass through verbatim, so the NOT NULL params
// column always receives well-formed JSON.
func TestDefaultParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   json.RawMessage
		want string
	}{
		{name: "nil becomes empty object", in: nil, want: "{}"},
		{name: "empty becomes empty object", in: json.RawMessage(""), want: "{}"},
		{name: "object passes through", in: json.RawMessage(`{"sort":"newest"}`), want: `{"sort":"newest"}`},
		{name: "array passes through", in: json.RawMessage(`[1,2,3]`), want: `[1,2,3]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := string(defaultParams(tt.in)); got != tt.want {
				t.Errorf("defaultParams(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
