package avatar

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"io"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

func NormalizeImage(input []byte) ([]byte, error) {
	if len(input) == 0 {
		return nil, ErrInvalidImage
	}
	if len(input) > MaxUploadBytes {
		return nil, ErrTooLarge
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(input))
	if err != nil || (format != "jpeg" && format != "png" && format != "webp") {
		return nil, ErrInvalidImage
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > MaxSide || config.Height > MaxSide || int64(config.Width)*int64(config.Height) > MaxPixels {
		return nil, ErrTooLarge
	}
	source, decodedFormat, err := image.Decode(bytes.NewReader(input))
	if err != nil || decodedFormat != format {
		return nil, ErrInvalidImage
	}
	if format == "jpeg" {
		source = orient(source, jpegOrientation(input))
	}
	bounds := source.Bounds()
	side := bounds.Dx()
	if bounds.Dy() < side {
		side = bounds.Dy()
	}
	startX := bounds.Min.X + (bounds.Dx()-side)/2
	startY := bounds.Min.Y + (bounds.Dy()-side)/2
	cropped := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.Draw(cropped, cropped.Bounds(), &image.Uniform{C: color.RGBA{R: 244, G: 247, B: 251, A: 255}}, image.Point{}, draw.Src)
	draw.Draw(cropped, cropped.Bounds(), source, image.Pt(startX, startY), draw.Over)
	output := image.NewRGBA(image.Rect(0, 0, OutputSize, OutputSize))
	xdraw.CatmullRom.Scale(output, output.Bounds(), cropped, cropped.Bounds(), draw.Src, nil)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, output, &jpeg.Options{Quality: JPEGQuality}); err != nil {
		return nil, ErrInvalidImage
	}
	return encoded.Bytes(), nil
}

func orient(source image.Image, orientation int) image.Image {
	b := source.Bounds()
	w, h := b.Dx(), b.Dy()
	if orientation < 2 || orientation > 8 {
		return source
	}
	outW, outH := w, h
	if orientation >= 5 {
		outW, outH = h, w
	}
	out := image.NewRGBA(image.Rect(0, 0, outW, outH))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := x, y
			switch orientation {
			case 2:
				dx = w - 1 - x
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dy = h - 1 - y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			out.Set(dx, dy, source.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out
}

func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	for offset := 2; offset+4 <= len(data); {
		if data[offset] != 0xff {
			break
		}
		marker := data[offset+1]
		offset += 2
		if marker == 0xda || marker == 0xd9 {
			break
		}
		if offset+2 > len(data) {
			break
		}
		length := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		if length < 2 || offset+length > len(data) {
			break
		}
		segment := data[offset+2 : offset+length]
		if marker == 0xe1 && len(segment) >= 6 && string(segment[:6]) == "Exif\x00\x00" {
			if value := tiffOrientation(segment[6:]); value >= 1 && value <= 8 {
				return value
			}
		}
		offset += length
	}
	return 1
}

func tiffOrientation(data []byte) int {
	if len(data) < 8 {
		return 1
	}
	var order binary.ByteOrder
	if string(data[:2]) == "II" {
		order = binary.LittleEndian
	} else if string(data[:2]) == "MM" {
		order = binary.BigEndian
	} else {
		return 1
	}
	if order.Uint16(data[2:4]) != 42 {
		return 1
	}
	offset := int(order.Uint32(data[4:8]))
	if offset < 0 || offset+2 > len(data) {
		return 1
	}
	count := int(order.Uint16(data[offset : offset+2]))
	for i := 0; i < count; i++ {
		entry := offset + 2 + i*12
		if entry+12 > len(data) {
			return 1
		}
		if order.Uint16(data[entry:entry+2]) == 0x0112 && order.Uint16(data[entry+2:entry+4]) == 3 && order.Uint32(data[entry+4:entry+8]) == 1 {
			return int(order.Uint16(data[entry+8 : entry+10]))
		}
	}
	return 1
}

func readLimited(reader io.Reader) ([]byte, error) {
	value, err := io.ReadAll(io.LimitReader(reader, MaxUploadBytes+1))
	if err != nil {
		return nil, ErrInvalidImage
	}
	if len(value) > MaxUploadBytes {
		return nil, ErrTooLarge
	}
	return value, nil
}
