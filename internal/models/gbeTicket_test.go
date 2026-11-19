package models

import (
	"encoding/base64"
	"encoding/binary"
	"strconv"
	"strings"
	"testing"
)

const testTicketSteamID uint64 = 76561198000000001

// gbeTicketBlob builds the AppTicket blob gbe_fork writes for a user without
// DLC and with a single empty license.
func gbeTicketBlob(t *testing.T, steamID uint64) []byte {
	t.Helper()
	var blob []byte
	blob = binary.LittleEndian.AppendUint32(blob, gbeTicketVersion)
	blob = binary.LittleEndian.AppendUint64(blob, steamID)
	blob = binary.LittleEndian.AppendUint32(blob, 1234) // AppId
	blob = binary.LittleEndian.AppendUint32(blob, 0)    // ExternalIP
	blob = binary.LittleEndian.AppendUint32(blob, 0)    // InternalIP
	blob = binary.LittleEndian.AppendUint32(blob, 0)    // AlwaysZero
	blob = binary.LittleEndian.AppendUint32(blob, 1)    // TicketGeneratedDate
	blob = binary.LittleEndian.AppendUint32(blob, 2)    // TicketGeneratedExpireDate
	blob = binary.LittleEndian.AppendUint16(blob, 1)    // licenses count
	blob = binary.LittleEndian.AppendUint32(blob, 0)    // license
	blob = binary.LittleEndian.AppendUint16(blob, 0)    // DLC count
	blob = binary.LittleEndian.AppendUint16(blob, 0)    // padding
	if len(blob) != gbeTicketStaticLen+4 {
		t.Fatalf("blob size = %d, want %d", len(blob), gbeTicketStaticLen+4)
	}
	return blob
}

// gbeOldTicket builds the pre-`new_app_ticket` ticket.
func gbeOldTicket(t *testing.T, steamID uint64) []byte {
	t.Helper()
	ticket := make([]byte, gbeOldTicketSize)
	binary.LittleEndian.PutUint32(ticket[0:4], gbeOldTicketHeader)
	binary.LittleEndian.PutUint64(ticket[4:12], 7) // ticket number
	binary.LittleEndian.PutUint64(ticket[12:20], steamID)
	binary.LittleEndian.PutUint32(ticket[20:24], 1) // generated date
	return ticket
}

// gbeNewTicket builds the current ticket, with the GC blob and/or the session
// blob that `use_gc_token` and `GetAuthSessionTicket` add.
func gbeNewTicket(t *testing.T, steamID uint64, gc bool, session bool) []byte {
	t.Helper()
	blob := gbeTicketBlob(t, steamID)
	layoutLen := uint32(4 + len(blob))

	var ticket []byte
	if gc {
		gcBlob := make([]byte, gbeGCSize)
		binary.LittleEndian.PutUint32(gcBlob[0:4], gbeOldTicketHeader)
		binary.LittleEndian.PutUint64(gcBlob[4:12], 7) // GCToken
		binary.LittleEndian.PutUint64(gcBlob[12:20], steamID)
		ticket = append(ticket, gcBlob...)
		if session {
			sessionBlob := make([]byte, gbeSessionSize)
			binary.LittleEndian.PutUint32(sessionBlob[0:4], gbeSessionSize-4)
			ticket = append(ticket, sessionBlob...)
		}
		// Length of everything that follows the GC layout: blob + signature.
		ticket = binary.LittleEndian.AppendUint32(ticket, layoutLen+gbeTicketSigLen)
	}
	ticket = binary.LittleEndian.AppendUint32(ticket, layoutLen)
	ticket = append(ticket, blob...)
	ticket = append(ticket, make([]byte, gbeTicketSigLen)...)
	return ticket
}

func TestSteamIDFromGbeTicket(t *testing.T) {
	tests := []struct {
		name   string
		ticket []byte
	}{
		{"old layout", gbeOldTicket(t, testTicketSteamID)},
		{"new layout without GC", gbeNewTicket(t, testTicketSteamID, false, false)},
		{"new layout with GC", gbeNewTicket(t, testTicketSteamID, true, false)},
		{"new layout with GC and session", gbeNewTicket(t, testTicketSteamID, true, true)},
	}
	want := strconv.FormatUint(testTicketSteamID, 10)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, encoding := range steamTicketEncodings {
				got := steamIDFromToken(encoding.EncodeToString(test.ticket))
				if got != want {
					t.Fatalf("steamIDFromToken() = %q, want %q", got, want)
				}
			}
		})
	}
}

func TestSteamIDFromTokenLeavesRealTicketsOpaque(t *testing.T) {
	// The real client sends a protobuf Encrypted App Ticket: version 2, a crc, a
	// five byte user data field, the ownership ticket length and a 160 byte AES
	// blob. Its SteamID is inside that blob, so it must not be read even when the
	// bytes happen to be present.
	ticket := []byte{0x08, 0x02, 0x10, 0x86, 0xc3, 0xd9, 0x89, 0x09, 0x18, 0x05, 0x20, 0x6b, 0x2a, 0xa0, 0x01}
	ticket = append(ticket, make([]byte, 160)...)
	binary.LittleEndian.PutUint64(ticket[40:48], testTicketSteamID)

	got := steamIDFromToken(base64.StdEncoding.EncodeToString(ticket))
	if !strings.HasPrefix(got, "token-") {
		t.Fatalf("steamIDFromToken() = %q, want a token-<hash> fallback", got)
	}
}

func TestSteamIDFromTokenRejectsMalformedGbeTicket(t *testing.T) {
	ticket := gbeNewTicket(t, testTicketSteamID, true, true)
	// The signature is missing, so the declared length no longer matches.
	got := steamIDFromToken(base64.StdEncoding.EncodeToString(ticket[:len(ticket)-gbeTicketSigLen-1]))
	if !strings.HasPrefix(got, "token-") {
		t.Fatalf("steamIDFromToken() = %q, want a token-<hash> fallback", got)
	}
}
