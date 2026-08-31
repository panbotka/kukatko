package ctl

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// TestParseCrop verifies the --crop spelling and the bounds it is rejected on.
func TestParseCrop(t *testing.T) {
	t.Parallel()

	crop, err := ParseCrop(" 0.1, 0.2,0.5 ,0.25")
	if err != nil {
		t.Fatalf("ParseCrop returned %v", err)
	}
	if crop != (Crop{X: 0.1, Y: 0.2, W: 0.5, H: 0.25}) {
		t.Errorf("crop = %+v, want the four parsed numbers", crop)
	}
	if got := crop.String(); got != "0.1,0.2,0.5,0.25" {
		t.Errorf("String() = %q, want a value --crop takes back", got)
	}

	// The crop columns are `real`, so what comes back from the database is the
	// float32 nearest the value written — 0.1 reads as 0.10000000149011612. It
	// must print as the number the user typed, not as that.
	noisy := Crop{X: 0.10000000149011612, Y: 0.10000000149011612, W: 0.800000011920929, H: 0.800000011920929}
	if got := noisy.String(); got != "0.1,0.1,0.8,0.8" {
		t.Errorf("String() of a float32 round trip = %q, want the clean rectangle", got)
	}

	for _, raw := range []string{"", "0.1,0.2,0.5", "a,b,c,d", "0.1,0.1,0,0.5", "-0.1,0,0.5,0.5", "0.6,0,0.5,0.5"} {
		if _, err := ParseCrop(raw); !errors.Is(err, ErrInvalidCrop) {
			t.Errorf("ParseCrop(%q) error = %v, want ErrInvalidCrop", raw, err)
		}
	}
}

// TestClient_GetImageEdit verifies the path and that the neutral edit a never
// edited photo answers with is recognised as neutral rather than as data.
func TestClient_GetImageEdit(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"photo_uid":"pht01","rotation":0,"brightness":0,"contrast":0}`))
	})

	raw, err := client.GetImageEdit(t.Context(), "pht01")
	if err != nil {
		t.Fatalf("GetImageEdit returned %v", err)
	}
	if gotPath != "/api/v1/photos/pht01/edit" {
		t.Errorf("path = %q, want the edit path", gotPath)
	}
	edit, err := DecodeImageEdit(raw)
	if err != nil {
		t.Fatalf("DecodeImageEdit returned %v", err)
	}
	if !edit.IsNeutral() || edit.Crop() != nil {
		t.Errorf("edit = %+v, want the neutral edit", edit)
	}
}

// TestClient_SetImageEdit verifies the write reads the stored edit first and
// sends the whole record back, so a rotation does not silently drop a crop.
func TestClient_SetImageEdit(t *testing.T) {
	t.Parallel()

	var methods []string
	var gotBody map[string]any
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"photo_uid":"pht01","crop_x":0.1,"crop_y":0.1,"crop_w":0.8,"crop_h":0.8,
				"rotation":0,"brightness":0.25,"contrast":0}`))
			return
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"photo_uid":"pht01","rotation":90}`))
	})

	rotation := 90
	raw, err := client.SetImageEdit(t.Context(), "pht01", ImageEditPatch{Rotation: &rotation})
	if err != nil {
		t.Fatalf("SetImageEdit returned %v", err)
	}
	if len(methods) != 2 || methods[0] != http.MethodGet || methods[1] != http.MethodPut {
		t.Fatalf("requests = %v, want a GET then a PUT", methods)
	}
	if gotBody["rotation"] != float64(90) {
		t.Errorf("body rotation = %v, want the new one", gotBody["rotation"])
	}
	if gotBody["crop_x"] != 0.1 || gotBody["crop_w"] != 0.8 || gotBody["brightness"] != 0.25 {
		t.Errorf("body = %v, want the stored crop and brightness carried across", gotBody)
	}
	if _, err := DecodeImageEdit(raw); err != nil {
		t.Errorf("DecodeImageEdit returned %v", err)
	}
}

// TestClient_SetImageEdit_clearCrop verifies --clear-crop sends four explicit
// nulls and leaves the adjustments alone.
func TestClient_SetImageEdit_clearCrop(t *testing.T) {
	t.Parallel()

	var gotBody map[string]json.RawMessage
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"photo_uid":"pht01","crop_x":0.1,"crop_y":0.1,"crop_w":0.8,"crop_h":0.8,
				"rotation":180,"brightness":0.25}`))
			return
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"photo_uid":"pht01","rotation":180}`))
	})

	if _, err := client.SetImageEdit(t.Context(), "pht01", ImageEditPatch{ClearCrop: true}); err != nil {
		t.Fatalf("SetImageEdit returned %v", err)
	}
	for _, key := range []string{"crop_x", "crop_y", "crop_w", "crop_h"} {
		if string(gotBody[key]) != "null" {
			t.Errorf("%s = %s, want an explicit null", key, gotBody[key])
		}
	}
	if string(gotBody["rotation"]) != "180" || string(gotBody["brightness"]) != "0.25" {
		t.Errorf("body = %v, want the rest of the edit untouched", gotBody)
	}
}

// TestClient_SetImageEdit_invalid verifies the range checks the API would answer
// 400 for are caught before the round trip — and before the audit entry.
func TestClient_SetImageEdit_invalid(t *testing.T) {
	t.Parallel()

	rotation := 45
	brightness := 2.0
	tests := []struct {
		name  string
		patch ImageEditPatch
		want  error
		serve bool
	}{
		{name: "nothing to change", patch: ImageEditPatch{}, want: ErrNoImageEdits},
		{name: "odd rotation", patch: ImageEditPatch{Rotation: &rotation}, want: ErrInvalidRotation, serve: true},
		{
			name:  "brightness out of range",
			patch: ImageEditPatch{Brightness: &brightness},
			want:  ErrInvalidAdjustment,
			serve: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
				if !tt.serve || r.Method != http.MethodGet {
					t.Errorf("the server was contacted with %s despite invalid input", r.Method)
					return
				}
				w.Write([]byte(`{"photo_uid":"pht01"}`))
			})
			if _, err := client.SetImageEdit(t.Context(), "pht01", tt.patch); !errors.Is(err, tt.want) {
				t.Errorf("SetImageEdit error = %v, want %v", err, tt.want)
			}
		})
	}
}

// TestClient_ResetImageEdit verifies the reset writes the neutral edit outright —
// it is a write, not the absence of one — and reads nothing first.
func TestClient_ResetImageEdit(t *testing.T) {
	t.Parallel()

	var methods []string
	var gotBody map[string]json.RawMessage
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"photo_uid":"pht01","rotation":0}`))
	})

	if _, err := client.ResetImageEdit(t.Context(), "pht01"); err != nil {
		t.Fatalf("ResetImageEdit returned %v", err)
	}
	if len(methods) != 1 || methods[0] != http.MethodPut {
		t.Fatalf("requests = %v, want one PUT and no read", methods)
	}
	if string(gotBody["crop_x"]) != "null" || string(gotBody["rotation"]) != "0" ||
		string(gotBody["brightness"]) != "0" || string(gotBody["contrast"]) != "0" {
		t.Errorf("body = %v, want the neutral edit stated in full", gotBody)
	}
}

// TestWriteImageEdit verifies the table names the neutral edit as such, so
// "everything is zero" cannot be mistaken for "nobody has looked".
func TestWriteImageEdit(t *testing.T) {
	t.Parallel()

	neutral, err := DecodeImageEdit([]byte(`{"photo_uid":"pht01"}`))
	if err != nil {
		t.Fatalf("DecodeImageEdit returned %v", err)
	}
	var buf bytes.Buffer
	if err := WriteImageEdit(&buf, neutral); err != nil {
		t.Fatalf("WriteImageEdit returned %v", err)
	}
	if !strings.Contains(buf.String(), "NEUTRAL") {
		t.Errorf("output %q does not say the edit is neutral", buf.String())
	}

	edited, err := DecodeImageEdit(
		[]byte(`{"photo_uid":"pht01","crop_x":0.1,"crop_y":0.2,"crop_w":0.5,"crop_h":0.5,"rotation":90}`))
	if err != nil {
		t.Fatalf("DecodeImageEdit returned %v", err)
	}
	buf.Reset()
	if err := WriteImageEdit(&buf, edited); err != nil {
		t.Fatalf("WriteImageEdit returned %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "NEUTRAL") {
		t.Errorf("output %q calls an edited photo neutral", out)
	}
	if strings.Contains(out, "0.100000001") {
		t.Errorf("output %q prints the float32 round-trip noise", out)
	}
	if !strings.Contains(out, "0.1,0.2,0.5,0.5") || !strings.Contains(out, "90°") {
		t.Errorf("output %q does not print the crop and the rotation", out)
	}
}

// TestDecodeImageEdit_invalid verifies malformed JSON surfaces as an error.
func TestDecodeImageEdit_invalid(t *testing.T) {
	t.Parallel()

	if _, err := DecodeImageEdit([]byte(`{"rotation":`)); err == nil {
		t.Error("DecodeImageEdit of malformed JSON returned no error")
	}
}
