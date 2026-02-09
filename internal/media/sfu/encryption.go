package sfu

import "fmt"

// EncryptionInfo holds E2EE metadata for a track or peer.
// The SFU never encrypts/decrypts — this is passed through for signaling.
type EncryptionInfo struct {
	Algorithm string
	KeyID     uint32
	SenderKey []byte
}

// ValidateE2EERoom checks whether a peer's encryption info is compatible
// with the room's E2EE requirement.
func ValidateE2EERoom(roomRequired bool, peerEnc *EncryptionInfo) error {
	if roomRequired && peerEnc == nil {
		return fmt.Errorf("room requires E2EE but peer has no encryption info")
	}
	if roomRequired && peerEnc.Algorithm == "" {
		return fmt.Errorf("room requires E2EE but peer has no encryption algorithm")
	}
	return nil
}
