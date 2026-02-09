package openai

import (
	"encoding/binary"
	"io"
)

// writeWAVHeader writes a minimal 44-byte WAV header for 16kHz 16-bit mono PCM.
func writeWAVHeader(w io.Writer, dataSize int) error {
	totalSize := 36 + dataSize

	// RIFF header
	if _, err := w.Write([]byte("RIFF")); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(totalSize)); err != nil {
		return err
	}
	if _, err := w.Write([]byte("WAVE")); err != nil {
		return err
	}

	// fmt sub-chunk
	if _, err := w.Write([]byte("fmt ")); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(16)); err != nil { // sub-chunk size
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(1)); err != nil { // PCM format
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(1)); err != nil { // mono
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(16000)); err != nil { // sample rate
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(32000)); err != nil { // byte rate (16000 * 1 * 2)
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(2)); err != nil { // block align (1 * 2)
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(16)); err != nil { // bits per sample
		return err
	}

	// data sub-chunk
	if _, err := w.Write([]byte("data")); err != nil {
		return err
	}
	return binary.Write(w, binary.LittleEndian, uint32(dataSize))
}
