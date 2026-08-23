package avatar

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestNormalizeImageProducesMetadataFreeSquareJPEG(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 800, 400))
	for y := range 400 {
		for x := range 800 {
			source.Set(x, y, color.NRGBA{R: uint8(x % 255), G: 80, B: 140, A: 128})
		}
	}
	var input bytes.Buffer
	if err := png.Encode(&input, source); err != nil {
		t.Fatal(err)
	}

	result, err := NormalizeImage(input.Bytes())
	if err != nil {
		t.Fatalf("NormalizeImage() erro = %v", err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(result))
	if err != nil || format != "jpeg" || config.Width != OutputSize || config.Height != OutputSize {
		t.Fatalf("saída = formato %q, %dx%d, erro %v", format, config.Width, config.Height, err)
	}
	if bytes.Contains(result, []byte("Exif")) {
		t.Fatal("saída preservou metadados EXIF")
	}
}

func TestNormalizeImageRejectsInvalidAndOversizedImages(t *testing.T) {
	if _, err := NormalizeImage([]byte("não é imagem")); err != ErrInvalidImage {
		t.Fatalf("conteúdo inválido: erro = %v", err)
	}
	tooWide := image.NewRGBA(image.Rect(0, 0, MaxSide+1, 1))
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, tooWide); err != nil {
		t.Fatal(err)
	}
	if _, err := NormalizeImage(encoded.Bytes()); err != ErrTooLarge {
		t.Fatalf("lado excessivo: erro = %v", err)
	}
}

func TestJPEGOrientationIsAppliedBeforeCrop(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 2, 1))
	source.Set(0, 0, color.RGBA{R: 255, A: 255})
	source.Set(1, 0, color.RGBA{B: 255, A: 255})
	rotated := orient(source, 6)
	if rotated.Bounds().Dx() != 1 || rotated.Bounds().Dy() != 2 {
		t.Fatalf("orientação 6 gerou limites %v", rotated.Bounds())
	}

	var raw bytes.Buffer
	if err := jpeg.Encode(&raw, source, nil); err != nil {
		t.Fatal(err)
	}
	exif := make([]byte, 32)
	copy(exif, []byte("Exif\x00\x00II"))
	binary.LittleEndian.PutUint16(exif[8:10], 42)
	binary.LittleEndian.PutUint32(exif[10:14], 8)
	binary.LittleEndian.PutUint16(exif[14:16], 1)
	binary.LittleEndian.PutUint16(exif[16:18], 0x0112)
	binary.LittleEndian.PutUint16(exif[18:20], 3)
	binary.LittleEndian.PutUint32(exif[20:24], 1)
	binary.LittleEndian.PutUint16(exif[24:26], 6)
	segment := append([]byte{0xff, 0xe1, 0, byte(len(exif) + 2)}, exif...)
	withEXIF := append(append([]byte{}, raw.Bytes()[:2]...), append(segment, raw.Bytes()[2:]...)...)
	if got := jpegOrientation(withEXIF); got != 6 {
		t.Fatalf("jpegOrientation() = %d, esperado 6", got)
	}
}
