// Package types is Metabolism's shared vocabulary: the records every stage of
// the digestive loop reads and writes.
//
// Nothing here knows about any particular host. A shell command, a support
// ticket and a research note all arrive as an Episode; what the host means by
// "tool" or "subject" is carried in plain strings and Attrs. See
// docs/architecture.md for how the concepts fit together.
//
// Enumerations are strings rather than integers so the journal stays readable
// with nothing but a SQLite shell, and so a record written by one version of
// the engine still decodes after a constant is added.
package types
