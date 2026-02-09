package openai

import (
	"encoding/binary"
)

// resample24to16 downsamples 24kHz 16-bit mono PCM to 16kHz using linear
// interpolation. Input and output are little-endian signed 16-bit samples.
func resample24to16(in []byte) []byte {
	inSamples := len(in) / 2
	if inSamples < 2 {
		return nil
	}

	// 24kHz -> 16kHz ratio = 3/2, so output has 2/3 the samples.
	outSamples := inSamples * 2 / 3
	out := make([]byte, outSamples*2)

	for i := 0; i < outSamples; i++ {
		// Source position in 24kHz stream (fractional).
		srcPos := float64(i) * 3.0 / 2.0
		srcIdx := int(srcPos)
		frac := srcPos - float64(srcIdx)

		s0 := readSample(in, srcIdx)
		s1 := readSample(in, srcIdx+1)

		// Linear interpolation.
		sample := int16(float64(s0) + frac*(float64(s1)-float64(s0)))
		binary.LittleEndian.PutUint16(out[i*2:], uint16(sample))
	}

	return out
}

func readSample(buf []byte, idx int) int16 {
	off := idx * 2
	if off+1 >= len(buf) {
		// Clamp to last sample.
		off = len(buf) - 2
	}
	if off < 0 {
		return 0
	}
	return int16(binary.LittleEndian.Uint16(buf[off:]))
}
