package httpapi

import (
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"

	_ "golang.org/x/image/webp"
)

const (
	maxMediaImageDimension = 16_384
	maxMediaImagePixels    = int64(40_000_000)
)

var (
	errMediaImageRead       = errors.New("unable to read uploaded image")
	errMediaImageDimensions = errors.New("uploaded image dimensions exceed the supported limit")
	errInvalidMediaImage    = errors.New("uploaded file is not a valid image")
)

type mediaImageReader struct {
	reader io.Reader
	err    error
}

func (reader *mediaImageReader) Read(buffer []byte) (int, error) {
	count, err := reader.reader.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) && reader.err == nil {
		reader.err = err
	}
	return count, err
}

func validateUploadedImage(source io.Reader, contentType string) error {
	reader := &mediaImageReader{reader: source}
	config, format, err := image.DecodeConfig(reader)
	if reader.err != nil {
		return fmt.Errorf("%w: %w", errMediaImageRead, reader.err)
	}
	if err != nil || imageContentType(format) != contentType {
		return errInvalidMediaImage
	}
	if config.Width <= 0 || config.Height <= 0 {
		return errInvalidMediaImage
	}
	if config.Width > maxMediaImageDimension || config.Height > maxMediaImageDimension ||
		int64(config.Width)*int64(config.Height) > maxMediaImagePixels {
		return errMediaImageDimensions
	}
	return nil
}

func imageContentType(format string) string {
	switch format {
	case "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	default:
		return ""
	}
}
