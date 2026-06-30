package photoapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// decodeEditBody runs decodeEdit over a raw JSON string via a test request.
func decodeEditBody(t *testing.T, body string) (editBody, error) {
	t.Helper()
	req := httptest.NewRequestWithContext(
		t.Context(), http.MethodPut, "/photos/p1/edit", strings.NewReader(body),
	)
	return decodeEdit(req)
}

func TestDecodeEdit_rejectsUnknownField(t *testing.T) {
	t.Parallel()
	if _, err := decodeEditBody(t, `{"sharpen": 1}`); err == nil {
		t.Error("decodeEdit accepted an unknown field, want error")
	}
}

func TestDecodeEdit_rejectsMalformedJSON(t *testing.T) {
	t.Parallel()
	if _, err := decodeEditBody(t, `{not json}`); err == nil {
		t.Error("decodeEdit accepted malformed JSON, want error")
	}
}

func TestDecodeEdit_valid(t *testing.T) {
	t.Parallel()
	body, err := decodeEditBody(t, `{"rotation": 90, "brightness": 0.2, "contrast": -0.1}`)
	if err != nil {
		t.Fatalf("decodeEdit: %v", err)
	}
	if body.Rotation != 90 || body.Brightness != 0.2 || body.Contrast != -0.1 {
		t.Errorf("decoded = %+v, want rotation 90 / brightness 0.2 / contrast -0.1", body)
	}
}

func TestValidateEdit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		body    editBody
		wantErr bool
	}{
		{name: "neutral edit is valid", body: editBody{}, wantErr: false},
		{name: "full rotation and colour", body: editBody{Rotation: 270, Brightness: 1, Contrast: -1}},
		{name: "invalid rotation", body: editBody{Rotation: 45}, wantErr: true},
		{name: "brightness too high", body: editBody{Brightness: 1.5}, wantErr: true},
		{name: "brightness too low", body: editBody{Brightness: -2}, wantErr: true},
		{name: "contrast out of range", body: editBody{Contrast: 2}, wantErr: true},
		{
			name: "valid crop",
			body: editBody{CropX: new(0.1), CropY: new(0.1), CropW: new(0.5), CropH: new(0.5)},
		},
		{
			name:    "partial crop rejected",
			body:    editBody{CropX: new(0.1), CropW: new(0.5)},
			wantErr: true,
		},
		{
			name:    "crop out of bounds",
			body:    editBody{CropX: new(0.6), CropY: new(0.0), CropW: new(0.6), CropH: new(0.5)},
			wantErr: true,
		},
		{
			name:    "crop with zero size",
			body:    editBody{CropX: new(0.0), CropY: new(0.0), CropW: new(0.0), CropH: new(0.5)},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateEdit(tt.body)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateEdit(%+v) error = %v, wantErr = %v", tt.body, err, tt.wantErr)
			}
		})
	}
}

func TestEditedFileName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "jpg keeps base", in: "beach.jpg", want: "beach.jpg"},
		{name: "heic becomes jpg", in: "IMG_1234.heic", want: "IMG_1234.jpg"},
		{name: "no extension gets jpg", in: "scan", want: "scan.jpg"},
		{name: "empty falls back", in: "", want: "download.jpg"},
		{name: "leading dot is not an extension", in: ".hidden", want: ".hidden.jpg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := editedFileName(tt.in); got != tt.want {
				t.Errorf("editedFileName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
