package codec

import (
	"encoding/binary"
	"io"

	"github.com/pion/opus"
)

// OpusToPCMWriter wraps an io.Writer and decodes Opus packets to 16kHz S16LE
// PCM before writing. Each Write call should contain exactly one Opus packet.
type OpusToPCMWriter struct {
	decoder *opus.Decoder
	dst     io.Writer
	pcmBuf  []byte // reusable buffer for decoded S16LE samples
}

// NewOpusToPCMWriter creates a writer that decodes Opus to 16kHz mono S16LE PCM.
func NewOpusToPCMWriter(dst io.Writer) *OpusToPCMWriter {
	return &OpusToPCMWriter{
		decoder: &opus.Decoder{},
		dst:     dst,
		// 48kHz Opus frame at 20ms = 960 samples. S16LE = 2 bytes/sample.
		// Output may be stereo (1920 samples). Pre-allocate generously.
		pcmBuf: make([]byte, 960*2*2),
	}
}

// Write decodes a single Opus packet and writes the resulting PCM to the
// underlying writer. The PCM output is 48kHz S16LE (matching Opus internal rate).
// If you need 16kHz, use OpusToPCM16Writer instead.
func (w *OpusToPCMWriter) Write(opusPacket []byte) (int, error) {
	_, _, err := w.decoder.Decode(opusPacket, w.pcmBuf)
	if err != nil {
		return 0, err
	}

	// Opus decodes to 48kHz. Write the full decoded buffer.
	// The ASR engine should be configured for the correct sample rate.
	n, err := w.dst.Write(w.pcmBuf)
	return n, err
}

// OpusToPCM16Writer decodes Opus to 16kHz mono S16LE PCM by downsampling.
// This is the format expected by most ASR engines (whisper, etc).
type OpusToPCM16Writer struct {
	decoder  *opus.Decoder
	dst      io.Writer
	pcmBuf48 []byte // 48kHz decoded samples
	pcmBuf16 []byte // 16kHz downsampled output
}

// NewOpusToPCM16Writer creates a writer that decodes Opus packets to 16kHz mono S16LE PCM.
func NewOpusToPCM16Writer(dst io.Writer) *OpusToPCM16Writer {
	return &OpusToPCM16Writer{
		decoder:  &opus.Decoder{},
		dst:      dst,
		pcmBuf48: make([]byte, 960*2*2),   // 20ms at 48kHz stereo = 1920 samples * 2 bytes
		pcmBuf16: make([]byte, 320*2),      // 20ms at 16kHz mono = 320 samples * 2 bytes
	}
}

// Write decodes a single Opus packet and writes 16kHz mono S16LE PCM to the
// underlying writer.
func (w *OpusToPCM16Writer) Write(opusPacket []byte) (int, error) {
	_, isStereo, err := w.decoder.Decode(opusPacket, w.pcmBuf48)
	if err != nil {
		return 0, err
	}

	// Downsample from 48kHz to 16kHz (ratio 3:1) and convert to mono if stereo.
	channels := 1
	if isStereo {
		channels = 2
	}
	samplesPerChannel := 960 // 20ms at 48kHz
	outSamples := samplesPerChannel / 3 // 320 samples at 16kHz

	if len(w.pcmBuf16) < outSamples*2 {
		w.pcmBuf16 = make([]byte, outSamples*2)
	}

	for i := 0; i < outSamples; i++ {
		srcIdx := i * 3 * channels * 2 // source sample index (S16LE, 2 bytes per sample)
		if srcIdx+1 >= len(w.pcmBuf48) {
			break
		}
		sample := int16(binary.LittleEndian.Uint16(w.pcmBuf48[srcIdx:]))
		if isStereo && srcIdx+3 < len(w.pcmBuf48) {
			// Average left and right channels for mono output.
			right := int16(binary.LittleEndian.Uint16(w.pcmBuf48[srcIdx+2:]))
			sample = int16((int32(sample) + int32(right)) / 2)
		}
		binary.LittleEndian.PutUint16(w.pcmBuf16[i*2:], uint16(sample))
	}

	return w.dst.Write(w.pcmBuf16[:outSamples*2])
}
