// SPDX-License-Identifier: AGPL-3.0-only

package models

import (
	"encoding/base64"
	"encoding/binary"
)

// Ticket layouts produced by gbe_fork (Goldberg Emulator). The emulator neither
// encrypts nor signs an Encrypted App Ticket, so a `Steam1.0` token carrying one
// of its tickets can be decoded directly. Tickets issued by the real Steam
// client keep the account id inside an AES-256 blob that cannot be opened
// without the publisher's key, so they are not decoded here.
const (
	// Old layout (enable_new_app_ticket = false):
	//   uint32 header (20) | uint64 ticket number | uint64 steam id | uint32 gen date
	gbeOldTicketSize          = 24
	gbeOldTicketHeader        = 20
	gbeOldTicketSteamIDOffset = 12

	// New layout (enable_new_app_ticket = true, the default):
	//   [AppTicketGC (24)] [AppTicketSession (28)] uint32 remaining length
	//   uint32 ticket data layout length | AppTicket blob | signature (128)
	gbeGCSize      = 24
	gbeSessionSize = 28
	// The ticket blob always starts with these fixed fields, so a shorter blob
	// is a layout mismatch and not a ticket.
	gbeTicketVersion   = 4
	gbeTicketStaticLen = 42
	gbeTicketSigLen    = 128
)

// steamTicketEncodings are the base64 alphabets a client may use for the token.
var steamTicketEncodings = []*base64.Encoding{
	base64.StdEncoding,
	base64.RawStdEncoding,
	base64.URLEncoding,
	base64.RawURLEncoding,
}

// gbeTicketSteamID returns the SteamID64 carried by a gbe_fork auth ticket. It
// reports false for anything that is not one of its two layouts, including the
// real Steam Encrypted App Ticket.
func gbeTicketSteamID(ticket []byte) (uint64, bool) {
	if steamID, ok := gbeOldTicketSteamID(ticket); ok {
		return steamID, true
	}
	// The ticket blob is preceded by its own length. Depending on the settings
	// that length sits at the start of the ticket, after the GC blob, or after
	// the GC and session blobs.
	for _, lengthOffset := range []int{0, gbeGCSize + 4, gbeGCSize + gbeSessionSize + 4} {
		if steamID, ok := gbeNewTicketSteamID(ticket, lengthOffset); ok {
			return steamID, true
		}
	}
	return 0, false
}

// gbeOldTicketSteamID decodes the pre-`new_app_ticket` layout, a fixed 24 bytes.
func gbeOldTicketSteamID(ticket []byte) (uint64, bool) {
	if len(ticket) != gbeOldTicketSize {
		return 0, false
	}
	if binary.LittleEndian.Uint32(ticket[0:4]) != gbeOldTicketHeader {
		return 0, false
	}
	return validSteamID64(binary.LittleEndian.Uint64(ticket[gbeOldTicketSteamIDOffset : gbeOldTicketSteamIDOffset+8]))
}

// gbeNewTicketSteamID decodes the current layout, reading the blob length from
// lengthOffset. The length includes its own four bytes; the signature is
// appended after the blob, so a valid ticket is exactly that long.
func gbeNewTicketSteamID(ticket []byte, lengthOffset int) (uint64, bool) {
	if lengthOffset+4 > len(ticket) {
		return 0, false
	}
	layoutLen := int(binary.LittleEndian.Uint32(ticket[lengthOffset : lengthOffset+4]))
	blobOffset := lengthOffset + 4
	if layoutLen < 4+gbeTicketStaticLen || lengthOffset+layoutLen+gbeTicketSigLen > len(ticket) {
		return 0, false
	}
	if binary.LittleEndian.Uint32(ticket[blobOffset:blobOffset+4]) != gbeTicketVersion {
		return 0, false
	}
	return validSteamID64(binary.LittleEndian.Uint64(ticket[blobOffset+4 : blobOffset+12]))
}

// validSteamID64 rejects values outside the range of an individual account, so
// a random byte sequence is not mistaken for an id.
func validSteamID64(value uint64) (uint64, bool) {
	if value < steamID64Min || value > steamID64Max {
		return 0, false
	}
	return value, true
}
