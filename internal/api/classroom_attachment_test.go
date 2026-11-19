package api

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"testing"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestInspectUploadValidImage(t *testing.T) {
	kind, err := inspectUploadFileReader(bytes.NewReader(testPNG(t)), "photo.png", 10<<20, 50<<20)
	if err != nil {
		t.Fatal(err)
	}
	if kind.Kind != "image" || kind.ContentType != "image/png" || kind.Limit != 10<<20 {
		t.Fatalf("kind = %#v", kind)
	}
}

func TestInspectUploadForbiddenTypes(t *testing.T) {
	for _, name := range []string{"payload.svg", "run.exe", "script.js", "page.html"} {
		if _, err := inspectUploadFileReader(bytes.NewReader([]byte("<svg/>")), name, 10<<20, 50<<20); err == nil {
			t.Fatalf("%s should be rejected", name)
		}
	}
}

func TestInspectUploadRejectsSpoofedImage(t *testing.T) {
	_, err := inspectUploadFileReader(bytes.NewReader([]byte("not a png")), "photo.png", 10<<20, 50<<20)
	inputErr, ok := err.(*uploadInputError)
	if !ok || inputErr.status != http.StatusUnsupportedMediaType {
		t.Fatalf("err = %#v", err)
	}
}

func TestInspectUploadAllowedFile(t *testing.T) {
	kind, err := inspectUploadFileReader(bytes.NewReader([]byte("%PDF-1.7\n")), "notes.pdf", 10<<20, 50<<20)
	if err != nil {
		t.Fatal(err)
	}
	if kind.Kind != "file" || kind.ContentType != "application/pdf" || kind.Limit != 50<<20 {
		t.Fatalf("kind = %#v", kind)
	}
}

func TestSanitizeFilename(t *testing.T) {
	if got := sanitizeFilename(`..\..\evil.txt`); got != "evil.txt" {
		t.Fatalf("got = %q", got)
	}
	if got := sanitizeFilename("a\x00b.txt"); got != "ab.txt" {
		t.Fatalf("got = %q", got)
	}
}
