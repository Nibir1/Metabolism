package types

// Provenance says where a piece of content came from. It is attached when an
// episode is ingested and carried onto everything derived from it, because a
// lesson is only as trustworthy as the least trustworthy thing it was learned
// from. This is the root of the immune system (docs/architecture.md): a lesson
// derived from low-trust content is quarantined, never injected directly.
type Provenance string

const (
	// ProvUserTyped is input the user typed.
	ProvUserTyped Provenance = "user-typed"
	// ProvUserVoice is a spoken request the host transcribed with confidence.
	ProvUserVoice Provenance = "user-voice"
	// ProvUserVoiceUnreliable is a transcript the host itself refused to act
	// on. It is a guess at what the user said, not a quotation.
	ProvUserVoiceUnreliable Provenance = "user-voice-unreliable"
	// ProvToolOutput is what a command or tool printed.
	ProvToolOutput Provenance = "tool-output"
	// ProvRetrievedWeb is text fetched from the network.
	ProvRetrievedWeb Provenance = "retrieved-web"
	// ProvFileContent is text read from a file, written by whoever wrote it.
	ProvFileContent Provenance = "file-content"
	// ProvSelf is content the host or engine produced itself.
	ProvSelf Provenance = "self"
)

// Valid reports whether p is a known provenance.
func (p Provenance) Valid() bool {
	switch p {
	case ProvUserTyped, ProvUserVoice, ProvUserVoiceUnreliable,
		ProvToolOutput, ProvRetrievedWeb, ProvFileContent, ProvSelf:
		return true
	}
	return false
}

// Trusted reports whether content with this provenance may become a lesson
// without passing through quarantine first. Only what the user said clearly,
// and what the system produced itself, qualifies. Tool output, files and web
// pages carry text written by third parties, and an unreliable transcript is a
// guess.
func (p Provenance) Trusted() bool {
	switch p {
	case ProvUserTyped, ProvUserVoice, ProvSelf:
		return true
	}
	return false
}
