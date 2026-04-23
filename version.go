package opencodesdk

// Version is the opencodesdk library version.
const Version = "0.0.0-dev"

// MinimumCLIVersion is the minimum required opencode CLI version. The
// SDK probes `opencode --version` while resolving the binary in
// Client.Start and fails fast with ErrUnsupportedCLIVersion unless
// WithSkipVersionCheck(true) is set.
const MinimumCLIVersion = "1.14.20"

// ProtocolVersion is the ACP protocol version this SDK targets.
const ProtocolVersion = 1
