package icy

// BuildMeta creates an ICY metadata block for the given stream title.
// If title is empty, returns a single null byte (no metadata).
func BuildMeta(title string) []byte {
	if title == "" {
		return []byte{0x00}
	}

	payload := "StreamTitle='" + title + "';"

	// Pad to 16-byte boundary
	padded := len(payload)
	if padded%16 != 0 {
		padded = padded + 16 - (padded % 16)
	}

	// Length byte = padded size / 16
	block := make([]byte, 1+padded)
	block[0] = byte(padded / 16)
	copy(block[1:], payload)
	// Remaining bytes are already zero (null padding)

	return block
}
