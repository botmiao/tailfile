package agent

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"io"
)

func zlibOrFlate(b []byte) (io.ReadCloser, error) {
	if z, err := zlib.NewReader(bytes.NewReader(b)); err == nil {
		return z, nil
	}
	return flate.NewReader(bytes.NewReader(b)), nil
}
