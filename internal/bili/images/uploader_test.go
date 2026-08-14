package images

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/FortyTwoCn/cyber-amber/internal/bili/client"
)

func TestUploadGIFMultipart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		file, header, err := r.FormFile("file_up")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		magic := make([]byte, 6)
		if _, err := file.Read(magic); err != nil || string(magic) != "GIF89a" {
			t.Fatalf("magic=%q err=%v", magic, err)
		}
		if header.Header.Get("Content-Type") != "image/gif" || r.FormValue("biz") != "draw" || r.FormValue("category") != "daily" || r.FormValue("csrf") != "csrf-value" {
			t.Fatalf("unexpected multipart: header=%v form=%v", header.Header, r.MultipartForm.Value)
		}
		fmt.Fprint(w, `{"code":0,"data":{"image_url":"//i0.hdslb.com/a.gif","image_width":"640","image_height":360,"image_size":"12.5"}}`)
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "artifact.gif")
	if err := os.WriteFile(path, []byte("GIF89a fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	uploader := New(client.New(server.Client(), server.URL, server.URL, "test"))
	result, err := uploader.Upload(context.Background(), path, "csrf-value")
	if err != nil {
		t.Fatal(err)
	}
	if result.URL != "https://i0.hdslb.com/a.gif" || result.Width != 640 || result.Height != 360 || result.SizeKB != 12.5 {
		t.Fatalf("unexpected result %#v", result)
	}
}

func TestUploadedImageRejectsFractionalDimensions(t *testing.T) {
	var image UploadedImage
	if err := json.Unmarshal([]byte(`{"image_url":"https://i0.hdslb.com/a.gif","image_width":640.5,"image_height":360,"size":12}`), &image); err == nil {
		t.Fatal("fractional image width was accepted")
	}
}

func TestUploadRejectsNonGIF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact.gif")
	if err := os.WriteFile(path, []byte("notgif"), 0o600); err != nil {
		t.Fatal(err)
	}
	uploader := New(client.New(nil, "http://invalid", "http://invalid", "test"))
	if _, err := uploader.Upload(context.Background(), path, "csrf"); err == nil {
		t.Fatal("expected non-GIF rejection")
	}
}
