package opencodesdk

// Version is the opencodesdk library version.
const Version = "0.0.0-dev"

// MinimumCLIVersion is the minimum required opencode CLI version. The
// SDK checks opencode's advertised `agentInfo.version` against this value
// during the ACP initialize handshake and fails fast on a mismatch unless
// WithSkipVersionCheck(true) is set.
const MinimumCLIVersion = "1.14.20"

// ProtocolVersion is the ACP protocol version this SDK targets.
const ProtocolVersion = 1
